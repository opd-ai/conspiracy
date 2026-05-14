package lora

import (
	"context"
	"fmt"
	"sync"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
)

// SX127x register addresses
const (
	RegFifo              = 0x00
	RegOpMode            = 0x01
	RegFrfMsb            = 0x06
	RegFrfMid            = 0x07
	RegFrfLsb            = 0x08
	RegPaConfig          = 0x09
	RegLna               = 0x0C
	RegFifoAddrPtr       = 0x0D
	RegFifoTxBaseAddr    = 0x0E
	RegFifoRxBaseAddr    = 0x0F
	RegFifoRxCurrentAddr = 0x10
	RegIrqFlags          = 0x12
	RegRxNbBytes         = 0x13
	RegPktSnrValue       = 0x19
	RegPktRssiValue      = 0x1A
	RegModemConfig1      = 0x1D
	RegModemConfig2      = 0x1E
	RegSymbTimeoutLsb    = 0x1F
	RegPreambleMsb       = 0x20
	RegPreambleLsb       = 0x21
	RegPayloadLength     = 0x22
	RegModemConfig3      = 0x26
	RegDioMapping1       = 0x40
	RegDioMapping2       = 0x41
	RegVersion           = 0x42
)

// OpMode values
const (
	ModeSleep        = 0x00
	ModeStandby      = 0x01
	ModeFsTx         = 0x02
	ModeTx           = 0x03
	ModeFsRx         = 0x04
	ModeRxContinuous = 0x05
	ModeRxSingle     = 0x06
	ModeLoRa         = 0x80
)

// IRQ flags
const (
	IrqTxDone     = 0x08
	IrqRxDone     = 0x40
	IrqPayloadCrc = 0x20
)

// SX127xSPI implements PacketRadio for SX127x chipsets via SPI.
type SX127xSPI struct {
	conn     spi.Conn
	resetPin gpio.PinOut
	dio0Pin  gpio.PinIn
	mu       sync.Mutex

	frequency float64
	sf        int
	bandwidth int
}

// NewSX127xSPI creates a new SX127x SPI driver.
// spiPort: SPI device path (e.g., "/dev/spidev0.0")
// resetPin: GPIO pin for RESET (e.g., "GPIO25")
// dio0Pin: GPIO pin for DIO0 interrupt (e.g., "GPIO24")
func NewSX127xSPI(spiPort, resetPin, dio0Pin string) (*SX127xSPI, error) {
	// Open SPI port
	p, err := spireg.Open(spiPort)
	if err != nil {
		return nil, fmt.Errorf("failed to open SPI port %s: %w", spiPort, err)
	}

	// Configure SPI for SX127x (max 10 MHz, Mode 0)
	conn, err := p.Connect(10*1000*1000, spi.Mode0, 8)
	if err != nil {
		return nil, fmt.Errorf("failed to connect SPI: %w", err)
	}

	radio := &SX127xSPI{
		conn:      conn,
		frequency: 868.1, // Default EU frequency
		sf:        10,
		bandwidth: 125,
	}

	// Initialize hardware
	if err := radio.reset(); err != nil {
		return nil, fmt.Errorf("failed to reset chip: %w", err)
	}

	// Verify chip version
	version, err := radio.readRegister(RegVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to read version: %w", err)
	}

	// Check for known SX127x chip versions
	switch version {
	case 0x12: // SX1276
	case 0x11: // SX1272
	case 0x22: // SX1277
	case 0x21: // SX1278
	case 0x24: // SX1279
	default:
		return nil, fmt.Errorf("unknown chip version: 0x%02X (expected SX127x)", version)
	}

	// Configure LoRa mode
	if err := radio.setLoRaMode(); err != nil {
		return nil, fmt.Errorf("failed to set LoRa mode: %w", err)
	}

	return radio, nil
}

// reset performs a hardware reset of the SX127x chip.
func (r *SX127xSPI) reset() error {
	if r.resetPin == nil {
		// Software reset fallback
		return r.writeRegister(RegOpMode, ModeSleep)
	}

	// Hardware reset sequence (per datasheet)
	r.resetPin.Out(gpio.Low)
	time.Sleep(1 * time.Millisecond)
	r.resetPin.Out(gpio.High)
	time.Sleep(10 * time.Millisecond)

	return nil
}

// readRegister reads a single register.
func (r *SX127xSPI) readRegister(addr byte) (byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tx := []byte{addr & 0x7F, 0x00} // MSB=0 for read
	rx := make([]byte, 2)

	if err := r.conn.Tx(tx, rx); err != nil {
		return 0, fmt.Errorf("SPI read failed: %w", err)
	}

	return rx[1], nil
}

// writeRegister writes a single register.
func (r *SX127xSPI) writeRegister(addr, value byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tx := []byte{addr | 0x80, value} // MSB=1 for write

	if err := r.conn.Tx(tx, nil); err != nil {
		return fmt.Errorf("SPI write failed: %w", err)
	}

	return nil
}

// setLoRaMode configures the chip for LoRa mode.
func (r *SX127xSPI) setLoRaMode() error {
	// Set sleep mode first
	if err := r.writeRegister(RegOpMode, ModeSleep); err != nil {
		return err
	}
	time.Sleep(10 * time.Millisecond)

	// Enable LoRa mode
	if err := r.writeRegister(RegOpMode, ModeSleep|ModeLoRa); err != nil {
		return err
	}

	// Set standby mode
	if err := r.writeRegister(RegOpMode, ModeStandby|ModeLoRa); err != nil {
		return err
	}

	return nil
}

// SetFrequency sets the radio frequency in MHz.
func (r *SX127xSPI) SetFrequency(mhz float64) error {
	r.frequency = mhz

	// Calculate frequency register value (Frf = Freq / (32MHz / 2^19))
	frf := uint32((mhz * 1000000.0) / (32000000.0 / 524288.0))

	if err := r.writeRegister(RegFrfMsb, byte(frf>>16)); err != nil {
		return err
	}
	if err := r.writeRegister(RegFrfMid, byte(frf>>8)); err != nil {
		return err
	}
	if err := r.writeRegister(RegFrfLsb, byte(frf)); err != nil {
		return err
	}

	return nil
}

// SetSpreadingFactor sets the spreading factor (7-12).
func (r *SX127xSPI) SetSpreadingFactor(sf int) error {
	if sf < 7 || sf > 12 {
		return fmt.Errorf("invalid spreading factor: %d (must be 7-12)", sf)
	}

	r.sf = sf

	// Read current config
	config2, err := r.readRegister(RegModemConfig2)
	if err != nil {
		return err
	}

	// Update SF bits (bits 7-4)
	config2 = (config2 & 0x0F) | byte(sf<<4)

	return r.writeRegister(RegModemConfig2, config2)
}

// SetBandwidth sets the bandwidth in kHz (125, 250, or 500).
func (r *SX127xSPI) SetBandwidth(khz int) error {
	var bwValue byte
	switch khz {
	case 125:
		bwValue = 0x70
	case 250:
		bwValue = 0x80
	case 500:
		bwValue = 0x90
	default:
		return fmt.Errorf("invalid bandwidth: %d (must be 125, 250, or 500)", khz)
	}

	r.bandwidth = khz

	// Read current config
	config1, err := r.readRegister(RegModemConfig1)
	if err != nil {
		return err
	}

	// Update BW bits (bits 7-4)
	config1 = (config1 & 0x0F) | bwValue

	return r.writeRegister(RegModemConfig1, config1)
}

// Send transmits a payload.
func (r *SX127xSPI) Send(ctx context.Context, payload []byte) error {
	if len(payload) > 255 {
		return fmt.Errorf("payload too large: %d bytes (max 255)", len(payload))
	}

	// Set standby mode
	if err := r.writeRegister(RegOpMode, ModeStandby|ModeLoRa); err != nil {
		return err
	}

	// Set FIFO pointer to TX base
	if err := r.writeRegister(RegFifoAddrPtr, 0x00); err != nil {
		return err
	}

	// Write payload to FIFO
	for i, b := range payload {
		if err := r.writeRegister(RegFifo, b); err != nil {
			return fmt.Errorf("failed to write byte %d: %w", i, err)
		}
	}

	// Set payload length
	if err := r.writeRegister(RegPayloadLength, byte(len(payload))); err != nil {
		return err
	}

	// Clear IRQ flags
	if err := r.writeRegister(RegIrqFlags, 0xFF); err != nil {
		return err
	}

	// Enter TX mode
	if err := r.writeRegister(RegOpMode, ModeTx|ModeLoRa); err != nil {
		return err
	}

	// Wait for TxDone (with context timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			flags, err := r.readRegister(RegIrqFlags)
			if err != nil {
				return err
			}

			if flags&IrqTxDone != 0 {
				// Clear flags and return to standby
				r.writeRegister(RegIrqFlags, 0xFF)
				r.writeRegister(RegOpMode, ModeStandby|ModeLoRa)
				return nil
			}
		}
	}
}

// Recv receives a payload.
func (r *SX127xSPI) Recv(ctx context.Context) ([]byte, error) {
	// Enter RX continuous mode
	if err := r.writeRegister(RegOpMode, ModeRxContinuous|ModeLoRa); err != nil {
		return nil, err
	}

	// Wait for RxDone
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.writeRegister(RegOpMode, ModeStandby|ModeLoRa)
			return nil, ctx.Err()
		case <-ticker.C:
			flags, err := r.readRegister(RegIrqFlags)
			if err != nil {
				return nil, err
			}

			if flags&IrqRxDone != 0 {
				// Check CRC
				if flags&IrqPayloadCrc != 0 {
					r.writeRegister(RegIrqFlags, 0xFF)
					return nil, fmt.Errorf("CRC error")
				}

				// Read payload length
				length, err := r.readRegister(RegRxNbBytes)
				if err != nil {
					return nil, err
				}

				// Get current RX address
				addr, err := r.readRegister(RegFifoRxCurrentAddr)
				if err != nil {
					return nil, err
				}

				// Set FIFO pointer
				if err := r.writeRegister(RegFifoAddrPtr, addr); err != nil {
					return nil, err
				}

				// Read payload
				payload := make([]byte, length)
				for i := range payload {
					payload[i], err = r.readRegister(RegFifo)
					if err != nil {
						return nil, err
					}
				}

				// Clear flags and return to standby
				r.writeRegister(RegIrqFlags, 0xFF)
				r.writeRegister(RegOpMode, ModeStandby|ModeLoRa)

				return payload, nil
			}
		}
	}
}

// RSSI returns the last packet RSSI in dBm.
func (r *SX127xSPI) RSSI() (int8, error) {
	rssi, err := r.readRegister(RegPktRssiValue)
	if err != nil {
		return 0, err
	}

	// Convert to dBm (per datasheet)
	return int8(-157 + int(rssi)), nil
}

// Close closes the SPI connection.
func (r *SX127xSPI) Close() error {
	// Return to sleep mode
	r.writeRegister(RegOpMode, ModeSleep)
	return nil
}
