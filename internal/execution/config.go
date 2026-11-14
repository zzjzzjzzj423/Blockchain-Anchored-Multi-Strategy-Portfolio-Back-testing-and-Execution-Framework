package execution

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

const costsRelativePath = "configs/costs.json"

type costModelDocument struct {
	CashTolerance     string `json:"cash_tolerance"`
	CommissionRate    string `json:"commission_rate"`
	InitialCash       string `json:"initial_cash"`
	MinimumNotional   string `json:"minimum_notional"`
	QuantityDecimals  int    `json:"quantity_decimals"`
	ResidualTolerance string `json:"residual_tolerance"`
	SchemaVersion     string `json:"schema_version"`
	SlippageRate      string `json:"slippage_rate"`
}

// LoadFrozenConfig reads the canonical M2 cost model and rejects drift from SPEC 5.
func LoadFrozenConfig(root, strategy string) (Config, error) {
	path := filepath.Join(root, filepath.FromSlash(costsRelativePath))
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open %s: %w", costsRelativePath, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var document costModelDocument
	if err := decoder.Decode(&document); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", costsRelativePath, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", costsRelativePath, err)
	}
	if document.SchemaVersion != "1.0.0" {
		return Config{}, fmt.Errorf("cost model schema_version must be 1.0.0")
	}
	parse := func(name, value string) (float64, error) {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, fmt.Errorf("parse %s: %w", name, err)
		}
		return parsed, nil
	}
	initialCash, err := parse("initial_cash", document.InitialCash)
	if err != nil {
		return Config{}, err
	}
	commission, err := parse("commission_rate", document.CommissionRate)
	if err != nil {
		return Config{}, err
	}
	slippage, err := parse("slippage_rate", document.SlippageRate)
	if err != nil {
		return Config{}, err
	}
	minimum, err := parse("minimum_notional", document.MinimumNotional)
	if err != nil {
		return Config{}, err
	}
	cashTolerance, err := parse("cash_tolerance", document.CashTolerance)
	if err != nil {
		return Config{}, err
	}
	residualTolerance, err := parse("residual_tolerance", document.ResidualTolerance)
	if err != nil {
		return Config{}, err
	}
	config := Config{
		Strategy:          strategy,
		InitialCash:       initialCash,
		CommissionRate:    commission,
		SlippageRate:      slippage,
		MinimumNotional:   minimum,
		QuantityDecimals:  document.QuantityDecimals,
		CashTolerance:     cashTolerance,
		ResidualTolerance: residualTolerance,
	}
	if err := validateConfig(config); err != nil {
		return Config{}, err
	}
	expected := DefaultConfig(strategy)
	if config != expected {
		return Config{}, fmt.Errorf("%s does not match frozen SPEC 5 defaults", costsRelativePath)
	}
	return config, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON documents are not allowed")
		}
		return err
	}
	return nil
}
