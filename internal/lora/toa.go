// Package lora provides time-on-air (ToA) calculation for LoRa transmissions.
package lora

import (
	"fmt"
	"math"
	"time"
)

// Calculate computes the time-on-air (ToA) for a LoRa transmission.
// Parameters:
//   - payloadBytes: Payload size in bytes
//   - sf: Spreading factor (7-12)
//   - bw: Bandwidth in kHz (125, 250, 500)
//   - cr: Coding rate (1-4, typically 1 for 4/5)
//
// Returns: Time-on-air duration
//
// Formula based on Semtech SX127x datasheet:
// ToA = preamble_time + payload_time
// where:
//
//	preamble_time = (preamble_symbols + 4.25) * symbol_duration
//	payload_time = payload_symbols * symbol_duration
//	symbol_duration = (2^sf) / bw
//	payload_symbols = 8 + max(ceil[(8*PL - 4*SF + 28 + 16 - 20*IH) / (4*(SF - 2*DE))] * (CR + 4), 0)
//
// Assumptions for this implementation:
//   - Preamble length: 8 symbols (standard)
//   - Implicit header (IH): 0 (explicit header mode)
//   - Low data rate optimize (DE): 1 when SF >= 11, else 0
//   - CRC enabled: yes (16 bits)
func Calculate(payloadBytes, sf, bw, cr int) (time.Duration, error) {
	if err := validateToAParameters(payloadBytes, sf, bw, cr); err != nil {
		return 0, err
	}

	symbolDuration := calculateSymbolDuration(sf, bw)
	preambleTime := calculatePreambleTime(symbolDuration)
	payloadTime := calculatePayloadTime(payloadBytes, sf, bw, cr, symbolDuration)

	toa := preambleTime + payloadTime
	return time.Duration(toa * float64(time.Second)), nil
}

// validateToAParameters checks parameter ranges.
func validateToAParameters(payloadBytes, sf, bw, cr int) error {
	if payloadBytes < 0 || payloadBytes > 255 {
		return fmt.Errorf("payload size must be 0-255 bytes (got %d)", payloadBytes)
	}

	if sf < 7 || sf > 12 {
		return fmt.Errorf("spreading factor must be 7-12 (got %d)", sf)
	}

	if bw != 125 && bw != 250 && bw != 500 {
		return fmt.Errorf("bandwidth must be 125, 250, or 500 kHz (got %d)", bw)
	}

	if cr < 1 || cr > 4 {
		return fmt.Errorf("coding rate must be 1-4 (got %d)", cr)
	}

	return nil
}

// calculateSymbolDuration computes the duration of one symbol in seconds.
func calculateSymbolDuration(sf, bw int) float64 {
	return math.Pow(2, float64(sf)) / (float64(bw) * 1000)
}

// calculatePreambleTime computes preamble transmission time in seconds.
func calculatePreambleTime(symbolDuration float64) float64 {
	const preambleSymbols = 8
	return (preambleSymbols + 4.25) * symbolDuration
}

// calculatePayloadTime computes payload transmission time in seconds.
func calculatePayloadTime(payloadBytes, sf, bw, cr int, symbolDuration float64) float64 {
	const implicitHeader = 0
	const crcOn = 1

	// Low data rate optimization enabled for SF11 and SF12
	de := 0
	if sf >= 11 && bw == 125 {
		de = 1
	}

	pl := float64(payloadBytes)
	sfFloat := float64(sf)
	crFloat := float64(cr)

	// Semtech formula for payload symbol count
	numerator := 8*pl - 4*sfFloat + 28 + 16*float64(crcOn) - 20*float64(implicitHeader)
	denominator := 4 * (sfFloat - 2*float64(de))

	// Calculate ceiling of division
	payloadSymbolsCeil := math.Ceil(numerator / denominator)

	// Multiply by (CR + 4) and ensure non-negative
	payloadSymbols := 8 + math.Max(payloadSymbolsCeil*(crFloat+4), 0)

	return payloadSymbols * symbolDuration
}

// CalculateWithDefaults computes ToA using typical default parameters:
// - Coding rate: 1 (4/5)
// - Bandwidth: 125 kHz
func CalculateWithDefaults(payloadBytes, sf int) (time.Duration, error) {
	return Calculate(payloadBytes, sf, 125, 1)
}
