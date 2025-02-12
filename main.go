package main

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cavaliergopher/grab/v3"
)

const (
	dataURL       = "https://plikplaski.mf.gov.pl/pliki//%s.7z"
	filePath      = "data.7z"
	extractPath   = "data/"
	activeFile    = "data/active_hashes.txt"
	exemptFile    = "data/exempt_hashes.txt"
	maskFilePath  = "data/maski.txt"
	iterations    = 5000
	serverAddress = ":8080"
)

var (
	activeHashes map[string]bool
	exemptHashes map[string]bool
	masks        []string
	mu           sync.RWMutex
)

// JSON response structure
type Response struct {
	Response string `json:"response"`
	Status   string `json:"status,omitempty"`
	Message  string `json:"message,omitempty"`
}

// Pobiera plik z Ministerstwa Finansów
func downloadFile() error {
	today := time.Now().Format("20060102")
	url := fmt.Sprintf(dataURL, today)

	resp, err := grab.Get(filePath, url)
	if err != nil {
		return fmt.Errorf("błąd pobierania pliku: %v", err)
	}
	fmt.Println("Pobrano plik:", resp.Filename)
	return nil
}

// Rozpakowuje plik .7z za pomocą 7-Zip
func extractFile() error {
	fmt.Println("Rozpakowywanie pliku...")
	cmd := exec.Command("7z", "x", filePath, "-o"+extractPath, "-y")
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("błąd rozpakowywania: %v", err)
	}
	fmt.Println("Plik rozpakowany.")
	return nil
}

// Wczytuje dane z pliku do mapy
func loadHashes(filePath string) (map[string]bool, error) {
	fmt.Println("Ładowanie hashy z pliku:", filePath)
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("błąd odczytu pliku hashy: %v", err)
	}

	hashes := make(map[string]bool)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line != "" {
			hashes[line] = true
		}
	}
	fmt.Println("Załadowano hashy:", len(hashes))
	return hashes, nil
}

// Wczytuje maski rachunków wirtualnych
func loadMasks() ([]string, error) {
	fmt.Println("Ładowanie masek rachunków...")
	data, err := ioutil.ReadFile(maskFilePath)
	if err != nil {
		return nil, fmt.Errorf("błąd odczytu pliku masek: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	masks := strings.Split(strings.TrimSpace(string(data)), "\n")

	fmt.Println("Załadowano masek:", len(masks))
	return masks, nil
}

// Generuje SHA-512
func calculateHash(input string) string {
	hash := sha512.Sum512([]byte(input))
	for i := 1; i < iterations; i++ {
		hash = sha512.Sum512(hash[:])
	}
	return hex.EncodeToString(hash[:])
}

// Tworzy warianty rachunku na podstawie masek
func generateMaskedAccounts(accountNumber string) []string {
	mu.RLock()
	defer mu.RUnlock()

	var maskedAccounts []string
	for _, mask := range masks {
		re := regexp.MustCompile("[XY]+")
		masked := re.ReplaceAllStringFunc(mask, func(s string) string {
			if strings.Contains(s, "Y") {
				start := strings.Index(mask, "Y")
				return accountNumber[start : start+len(s)]
			}
			return strings.Repeat("X", len(s))
		})
		maskedAccounts = append(maskedAccounts, masked)
	}
	return maskedAccounts
}

// Obsługuje endpoint /verify
func verifyHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	nip := query.Get("nip")
	accountNumber := query.Get("account")

	if nip == "" {
		json.NewEncoder(w).Encode(Response{Response: "ERROR", Message: "Brak wymaganych parametrów"})
		return
	}

	hashInput := nip
	if accountNumber != "" {
		hashInput += accountNumber
	}

	hash := calculateHash(hashInput)

	mu.RLock()
	defer mu.RUnlock()

	// Sprawdzenie aktywnych podatników
	if activeHashes[hash] {
		json.NewEncoder(w).Encode(Response{Response: "OK", Status: "ACTIVE"})
		return
	}

	// Sprawdzenie podatników zwolnionych
	if exemptHashes[hash] {
		json.NewEncoder(w).Encode(Response{Response: "OK", Status: "EXEMPT"})
		return
	}

	// Sprawdzenie rachunku wirtualnego (maski)
	maskedAccounts := generateMaskedAccounts(accountNumber)
	for _, masked := range maskedAccounts {
		maskedInput := nip + masked
		maskedHash := calculateHash(maskedInput)
		if activeHashes[maskedHash] {
			json.NewEncoder(w).Encode(Response{Response: "OK", Status: "ACTIVE"})
			return
		}
		if exemptHashes[maskedHash] {
			json.NewEncoder(w).Encode(Response{Response: "OK", Status: "EXEMPT"})
			return
		}
	}

	json.NewEncoder(w).Encode(Response{Response: "OK", Status: "NOTFOUND"})
}

// Codzienna aktualizacja danych
func updateData() {
	for {
		fmt.Println("Rozpoczęcie aktualizacji danych...")
		if err := downloadFile(); err != nil {
			fmt.Println("Błąd pobierania pliku:", err)
			time.Sleep(24 * time.Hour)
			continue
		}

		if err := extractFile(); err != nil {
			fmt.Println("Błąd rozpakowywania:", err)
			time.Sleep(24 * time.Hour)
			continue
		}

		// Wczytanie hashy
		var err error
		activeHashes, err = loadHashes(activeFile)
		if err != nil {
			fmt.Println("Błąd ładowania hashy aktywnych:", err)
		}

		exemptHashes, err = loadHashes(exemptFile)
		if err != nil {
			fmt.Println("Błąd ładowania hashy zwolnionych:", err)
		}

		// Wczytanie masek
		masks, err = loadMasks()
		if err != nil {
			fmt.Println("Błąd ładowania masek:", err)
		}

		time.Sleep(24 * time.Hour) // Pobieranie codziennie
	}
}

func main() {
	go updateData()

	http.HandleFunc("/verify", verifyHandler)
	fmt.Println("Serwer HTTP działa na", serverAddress)
	log.Fatal(http.ListenAndServe(serverAddress, nil))
}
