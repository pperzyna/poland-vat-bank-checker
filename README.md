# Poland VAT & Bank Checker

```sh
docker run -p 8080:8080 ghcr.io/pperzyna/poland-vat-bank-checker
```

## Overview

**pl-vatbank-checker** is a Go-based web service that verifies VAT taxpayers' NIP (Tax Identification Number) and bank account numbers using the official Polish Ministry of Finance [flat file](https://www.podatki.gov.pl/vat/bezpieczna-transakcja/wykaz-podatnikow-vat/plik-plaski/) data.

The service provides a REST API endpoint to check whether a given NIP and bank account number exist in the registry as active, exempt, or not found.

## Features

- ✅ Daily automatic download and extraction of the latest VAT taxpayer flat file
- ✅ Support different bank account masks
- ✅ Optimized query performance (but RAM consuming)
- ✅ JSON API responses
- ✅ Container support for easy deployment

## API Endpoints

### Verify a NIP and Bank Account

#### Request

```sh
GET /verify?nip=<NIP>&bank=<BANK_ACCOUNT>
```

#### Response Examples

**1. Active taxpayer:**

```json
{ "response": "OK", "status": "ACTIVE", "bank": "MATCHED", "date": "20250101" }
```

**2. Exempt taxpayer:**

```json
{ "response": "OK", "status": "EXEMPT", "bank": "MATCHED", "date": "20250101" }
```

**4. Taxpayer without bank:**

```json
{
  "response": "OK",
  "status": "ACTIVE or EXEMPT",
  "bank": "NA",
  "date": "20250101"
}
```

**5. Not found in registry:**

```json
{
  "response": "OK",
  "status": "NOT_FOUND",
  "bank": "NOT_FOUND",
  "date": "20250101"
}
```

**6. Error response:**

```json
{ "response": "ERROR", "message": "Invalid parameters" }
```

## Installation & Setup

### Prerequisites

- Go 1.24+
- `p7zip-full` (for extracting `.7z` files)
- Docker (optional, for containerized deployment)

### Local Setup

```sh
git clone https://github.com/pperzyna/poland-vat-bank-checker.git
cd poland-vat-bank-checker

# Install dependencies
go mod tidy

# Run the application
go run main.go
```

### Docker Setup

```sh
docker build -t pl-vatbank-checker .
docker run -p 8080:8080 pl-vatbank-checker
```

## How It Works

1. The program downloads the latest flat file from the Ministry of Finance.
2. Extracts the `.7z` archive to retrieve taxpayer data.
3. Loads the hash data and account masks into memory.
4. Listens on `:8080` for API requests.
5. Verifies NIP and bank account numbers using SHA-512 hashing.

## Troubleshooting

### 7-Zip Not Found Error

If you get the error:

```sh
exec: "7z": executable file not found in $PATH
```

Install `p7zip`:

- **Ubuntu/Debian:** `sudo apt install p7zip-full -y`
- **MacOS:** `brew install p7zip`
- **Alpine Linux:** `apk add p7zip`
