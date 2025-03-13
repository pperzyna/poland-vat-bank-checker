package main

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cavaliergopher/grab/v3"
)

const (
	dataURL       = "https://plikplaski.mf.gov.pl/pliki/{DATE}.7z"
	serverAddress = ":8080"
)

var (
	dataDate     string = "20250101"
	iterations   int    = 5000
	activeHashes map[string]bool
	exemptHashes map[string]bool
	masks        []string
	mu           sync.RWMutex
)

// JSON Structure
type DataStructure struct {
	Header struct {
		DataDate       string `json:"dataGenerowaniaDanych"`
		TransformCount string `json:"liczbaTransformacji"`
	} `json:"naglowek"`
	ActiveHashes []string `json:"skrotyPodatnikowCzynnych"`
	ExemptHashes []string `json:"skrotyPodatnikowZwolnionych"`
	Masks        []string `json:"maski"`
}

// JSON Response Structure
type Response struct {
	Response string `json:"response"`
	Status   string `json:"status,omitempty"`
	Bank     string `json:"bank,omitempty"`
	Date     string `json:"date,omitempty"`
	Message  string `json:"message,omitempty"`
}

// 📌 Download the latest VAT file
func downloadFile() (string, error) {
	today := time.Now().Format("20060102")
	url := strings.ReplaceAll(dataURL, "{DATE}", today)
	fileName := today + ".7z"

	log.Printf("[INFO] Downloading: %s", url)
	resp, err := grab.Get(fileName, url)
	if err != nil {
		log.Printf("[ERROR] Download failed: %v", err)
		return "", err
	}
	log.Printf("[INFO] Downloaded: %s", resp.Filename)
	return fileName, nil
}

// 📌 Extract the JSON file from the `.7z` archive
func extractFile(file string) (string, error) {
	log.Printf("[INFO] Extracting JSON file from %s", file)

	cmd := exec.Command("7z", "x", file, "-y")
	err := cmd.Run()
	if err != nil {
		log.Printf("[ERROR] Extraction failed: %v", err)
		return "", err
	}

	jsonPath := strings.Replace(file, ".7z", ".json", 1)
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		log.Printf("[ERROR] Extracted JSON file not found: %s", jsonPath)
		return "", err
	}

	log.Printf("[INFO] Extracted JSON file: %s", jsonPath)
	return jsonPath, nil
}

// 📌 Load and parse JSON file
func loadData(jsonPath string) error {
	log.Printf("[INFO] Loading data from JSON: %s", jsonPath)

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		log.Printf("[ERROR] Reading JSON file failed: %v", err)
		return err
	}

	var structure DataStructure
	if err := json.Unmarshal(data, &structure); err != nil {
		log.Printf("[ERROR] Parsing JSON failed: %v", err)
		return err
	}

	mu.Lock()
	defer mu.Unlock()

	dataDate = structure.Header.DataDate
	if parsedIterations, err := strconv.Atoi(structure.Header.TransformCount); err == nil && parsedIterations > 0 {
		iterations = parsedIterations
	} else {
		log.Printf("[WARNING] Unable to parse TransformCount, using default (%d)", iterations)
	}

	// Store data in memory
	activeHashes = make(map[string]bool, len(structure.ActiveHashes))
	for _, hash := range structure.ActiveHashes {
		activeHashes[hash] = true
	}

	exemptHashes = make(map[string]bool, len(structure.ExemptHashes))
	for _, hash := range structure.ExemptHashes {
		exemptHashes[hash] = true
	}

	masks = structure.Masks

	log.Printf("[INFO] Loaded %d active hashes, %d exempt hashes, %d masks. Data date: %s, Iterations: %d",
		len(activeHashes), len(exemptHashes), len(masks), dataDate, iterations)

	return nil
}

// 📌 Generate SHA-512 Hash
func calculateHash(input string) string {
	hash := []byte(input)

	for i := 0; i < iterations; i++ {
		hashSum := sha512.Sum512(hash)
		hash = []byte(strings.ToLower(hex.EncodeToString(hashSum[:])))
	}

	return string(hash)
}

// 📌 Apply a mask to an account number
func applyMask(bank string, mask string) string {
	maskedResult := []rune(mask)
	accountDigits := []rune(bank)

	for i, char := range maskedResult {
		if char == 'Y' {
			// Replace 'Y' with the corresponding digit from the account number			
			maskedResult[i] = accountDigits[i]
		} else if char == 'X' {
			// Keep 'X' as it represents a placeholder
			maskedResult[i] = 'X'
		}
	}

	return string(maskedResult)
}

// 📌 Handle /verify API endpoint
func verifyHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	nip := query.Get("nip")
	bank := query.Get("bank")

	if nip == "" {
		json.NewEncoder(w).Encode(Response{Response: "ERROR", Message: "Missing required parameters"})
		return
	}
	if bank != "" && len(bank) != 26 {
		json.NewEncoder(w).Encode(Response{Response: "ERROR", Message: "Invalid bank account number"})
		return
	}

	mu.RLock()
	currentDataDate := dataDate
	mu.RUnlock()

	hashed := calculateHash(currentDataDate + nip)
	// log.Printf("[INFO] Verifying NIP: %s, Hash: %s", nip, hashed)

	mu.RLock()
	_, isActive := activeHashes[hashed]
	_, isExempt := exemptHashes[hashed]
	mu.RUnlock()

	if isActive {
		json.NewEncoder(w).Encode(Response{Response: "OK", Status: "ACTIVE", Bank: "NA", Date: currentDataDate})
		return
	}
	if isExempt {
		json.NewEncoder(w).Encode(Response{Response: "OK", Status: "EXEMPT", Bank: "NA", Date: currentDataDate})
		return
	}

	if bank != "" {
		hashed = calculateHash(currentDataDate + nip + bank)
		// log.Printf("[INFO] Verifying NIP: %s, Bank: %s, Hash: %s", nip, bank, hashed)

		mu.RLock()
		_, isActiveBank := activeHashes[hashed]
		_, isExemptBank := exemptHashes[hashed]
		mu.RUnlock()

		if isActiveBank {
			json.NewEncoder(w).Encode(Response{Response: "OK", Status: "ACTIVE", Bank: "MATCHED", Date: currentDataDate})
			return
		}
		if isExemptBank {
			json.NewEncoder(w).Encode(Response{Response: "OK", Status: "EXEMPT", Bank: "MATCHED", Date: currentDataDate})
			return
		}

		for _, mask := range masks {
			masked := applyMask(bank, mask)
			maskedHash := calculateHash(currentDataDate + nip + masked)
			// log.Printf("[INFO] Verifying NIP: %s, Bank: %s, Mask: %s, Masked: %s, Hash: %s", nip, bank, mask, masked, maskedHash)

			mu.RLock()
			_, isActiveMasked := activeHashes[maskedHash]
			_, isExemptMasked := exemptHashes[maskedHash]
			mu.RUnlock()

			if isActiveMasked {
				json.NewEncoder(w).Encode(Response{Response: "OK", Status: "ACTIVE", Bank: "MATCHED", Date: currentDataDate})
				return
			}
			if isExemptMasked {
				json.NewEncoder(w).Encode(Response{Response: "OK", Status: "EXEMPT", Bank: "MATCHED", Date: currentDataDate})
				return
			}
		}
	}

	json.NewEncoder(w).Encode(Response{Response: "OK", Status: "NOT_FOUND", Bank: "NOT_FOUND", Date: currentDataDate})
}

// 📌 Handle /health API endpoint
func healthHandler(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(Response{Response: "OK", Message: "Service is running"})
}

// 📌 Periodic data update
func updateData() {
	for {
		log.Printf("[INFO] Starting data update...")
		file, err := downloadFile()
		if err != nil {
			log.Printf("[ERROR] Download failed: %s", err)
			time.Sleep(1 * time.Hour)
			continue
		}

		jsonFile, err := extractFile(file)
		if err != nil {
			log.Printf("[ERROR] Extraction failed: %s", err)
			time.Sleep(1 * time.Hour)
			continue
		}

		if err := loadData(jsonFile); err != nil {
			log.Printf("[ERROR] Loading failed: %s", err)
			time.Sleep(1 * time.Hour)
			continue
		}

		_ = os.Remove(file)
		_ = os.Remove(jsonFile)

		log.Printf("[INFO] Data update completed successfully.")
		time.Sleep(24 * time.Hour)
	}
}

// 📌 Handle Graceful Shutdown
func handleShutdown() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop
	log.Printf("[INFO] Shutting down server...")
	os.Exit(0)
}

func main() {
	go updateData()
	go handleShutdown()

	http.HandleFunc("/verify", verifyHandler)
	http.HandleFunc("/health", healthHandler)
	log.Printf("[INFO] Server running at %s", serverAddress)
	log.Fatal(http.ListenAndServe(serverAddress, nil))
}
