package lora

import (
	"context"
	"fmt"
	"net"
	"sync"
)

// UDPRadio implements PacketRadio using UDP sockets for testing without hardware.
// This allows development and integration testing without LoRa hardware.
type UDPRadio struct {
	conn   net.PacketConn // Use interface type per guideline #1
	peer   net.Addr       // Use interface type per guideline #1
	mu     sync.Mutex
	closed bool

	// Simulated radio parameters
	frequency float64
	sf        int
	bandwidth int
	rssi      int8
}

// NewUDPRadio creates a UDP-based LoRa simulator for testing.
// listenAddr should be "host:port" (e.g., "127.0.0.1:8001")
// peerAddr should be "host:port" of the remote node
func NewUDPRadio(listenAddr, peerAddr string) (*UDPRadio, error) {
	conn, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", listenAddr, err)
	}

	peer, err := net.ResolveUDPAddr("udp", peerAddr)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to resolve peer %s: %w", peerAddr, err)
	}

	return &UDPRadio{
		conn:      conn,
		peer:      peer,
		frequency: 868.1, // Default EU frequency
		sf:        10,    // Default SF10
		bandwidth: 125,   // Default 125 kHz
		rssi:      -50,   // Simulated RSSI
	}, nil
}

// Send transmits a payload over UDP.
func (r *UDPRadio) Send(ctx context.Context, payload []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return fmt.Errorf("radio closed")
	}

	_, err := r.conn.WriteTo(payload, r.peer)
	if err != nil {
		return fmt.Errorf("failed to send: %w", err)
	}
	return nil
}

// Recv receives a payload from UDP.
func (r *UDPRadio) Recv(ctx context.Context) ([]byte, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, fmt.Errorf("radio closed")
	}
	r.mu.Unlock()

	buf := make([]byte, 256) // Max LoRa payload size

	// Set read deadline based on context
	if deadline, ok := ctx.Deadline(); ok {
		r.conn.SetReadDeadline(deadline)
	}

	n, _, err := r.conn.ReadFrom(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to recv: %w", err)
	}

	return buf[:n], nil
}

// Close closes the UDP connection.
func (r *UDPRadio) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	r.closed = true
	return r.conn.Close()
}

// SetFrequency simulates setting the radio frequency.
func (r *UDPRadio) SetFrequency(mhz float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frequency = mhz
	return nil
}

// SetSpreadingFactor simulates setting the spreading factor.
func (r *UDPRadio) SetSpreadingFactor(sf int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if sf < 7 || sf > 12 {
		return fmt.Errorf("invalid spreading factor: %d (must be 7-12)", sf)
	}

	r.sf = sf
	return nil
}

// SetBandwidth simulates setting the bandwidth.
func (r *UDPRadio) SetBandwidth(khz int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if khz != 125 && khz != 250 && khz != 500 {
		return fmt.Errorf("invalid bandwidth: %d (must be 125, 250, or 500)", khz)
	}

	r.bandwidth = khz
	return nil
}

// RSSI returns simulated RSSI.
func (r *UDPRadio) RSSI() (int8, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rssi, nil
}
