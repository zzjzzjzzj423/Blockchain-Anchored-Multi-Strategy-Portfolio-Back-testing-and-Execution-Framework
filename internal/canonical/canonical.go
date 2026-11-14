package canonical

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const decimalPlaces = 12

// Decimal12 formats a finite number with the fixed precision required by SPEC 13.2.
func Decimal12(value float64) (string, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "", fmt.Errorf("non-finite number: %v", value)
	}
	return DecimalString12(strconv.FormatFloat(value, 'g', -1, 64))
}

// DecimalString12 quantizes a decimal string exactly with round-half-to-even.
func DecimalString12(value string) (string, error) {
	rational, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok {
		return "", fmt.Errorf("invalid decimal number %q", value)
	}

	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(decimalPlaces), nil)
	scaledNumerator := new(big.Int).Mul(rational.Num(), scale)
	negative := scaledNumerator.Sign() < 0
	absNumerator := new(big.Int).Abs(scaledNumerator)
	denominator := rational.Denom()
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(absNumerator, denominator, remainder)

	twiceRemainder := new(big.Int).Lsh(new(big.Int).Set(remainder), 1)
	comparison := twiceRemainder.Cmp(denominator)
	if comparison > 0 || (comparison == 0 && quotient.Bit(0) == 1) {
		quotient.Add(quotient, big.NewInt(1))
	}
	if quotient.Sign() == 0 {
		negative = false
	}

	digits := quotient.String()
	if len(digits) <= decimalPlaces {
		digits = strings.Repeat("0", decimalPlaces+1-len(digits)) + digits
	}
	integerPart := digits[:len(digits)-decimalPlaces]
	fractionalPart := digits[len(digits)-decimalPlaces:]
	prefix := ""
	if negative {
		prefix = "-"
	}
	return prefix + integerPart + "." + fractionalPart, nil
}

// JSON converts a JSON-compatible Go value into deterministic, compact JSON.
// The decode-to-map step makes encoding/json apply recursive lexical key ordering.
func JSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal intermediate JSON: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode intermediate JSON: %w", err)
	}
	document, err = normalizeJSON(document)
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("encode canonical JSON: %w", err)
	}
	return output.Bytes(), nil
}

func normalizeJSON(value any) (any, error) {
	switch typed := value.(type) {
	case string:
		return norm.NFC.String(typed), nil
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			normalized, err := normalizeJSON(item)
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			normalizedKey := norm.NFC.String(key)
			if _, exists := result[normalizedKey]; exists {
				return nil, fmt.Errorf("JSON keys collide after NFC normalization")
			}
			normalized, err := normalizeJSON(item)
			if err != nil {
				return nil, err
			}
			result[normalizedKey] = normalized
		}
		return result, nil
	default:
		return value, nil
	}
}
