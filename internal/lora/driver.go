// Package lora provides LoRa radio driver abstraction and frame codec.
package lora

import "context"

// PacketRadio defines the interface for LoRa radio communication.
// Implementations include SPI (SX127x/SX126x chipsets), UART, USB-Serial, and UDP test stub.
type PacketRadio interface {
	Send(ctx context.Context, payload []byte) error
	Recv(ctx context.Context) ([]byte, error)
	Close() error
	SetFrequency(mhz float64) error
	SetSpreadingFactor(sf int) error // 7-12
	SetBandwidth(khz int) error      // 125, 250, 500
	RSSI() (int8, error)             // dBm
}
