#!/usr/bin/env bash
# Conspiracy mesh network daemon uninstallation script
# Usage: sudo bash uninstall.sh [--purge]

set -euo pipefail

# Configuration
CONFIG_DIR="/etc/conspiracyd"
STATE_DIR="/var/lib/conspiracyd"
BINARY_DEST="/usr/sbin/conspiracyd"
SYSTEMD_DEST="/etc/systemd/system/conspiracyd.service"
PURGE_MODE=false

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Parse arguments
for arg in "$@"; do
    case $arg in
        --purge)
            PURGE_MODE=true
            shift
            ;;
        *)
            echo "Unknown option: $arg"
            echo "Usage: $0 [--purge]"
            echo "  --purge: Remove configuration and state data"
            exit 1
            ;;
    esac
done

# Helper functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root (use sudo)"
        exit 1
    fi
}

stop_and_disable_service() {
    if systemctl is-active --quiet conspiracyd; then
        log_info "Stopping conspiracyd service"
        systemctl stop conspiracyd
    fi
    
    if systemctl is-enabled --quiet conspiracyd 2>/dev/null; then
        log_info "Disabling conspiracyd service"
        systemctl disable conspiracyd
    fi
}

remove_systemd_unit() {
    if [[ -f "$SYSTEMD_DEST" ]]; then
        log_info "Removing systemd unit file"
        rm -f "$SYSTEMD_DEST"
        systemctl daemon-reload
    fi
}

remove_binary() {
    if [[ -f "$BINARY_DEST" ]]; then
        log_info "Removing conspiracyd binary"
        rm -f "$BINARY_DEST"
    fi
}

remove_config_and_state() {
    if [[ "$PURGE_MODE" == "true" ]]; then
        if [[ -d "$CONFIG_DIR" ]]; then
            log_warn "Removing configuration directory: $CONFIG_DIR"
            rm -rf "$CONFIG_DIR"
        fi
        
        if [[ -d "$STATE_DIR" ]]; then
            log_warn "Removing state directory: $STATE_DIR"
            rm -rf "$STATE_DIR"
        fi
    else
        log_info "Keeping configuration and state data (use --purge to remove)"
    fi
}

print_summary() {
    cat <<EOF

${GREEN}Uninstallation complete!${NC}

Removed:
- Systemd service: $SYSTEMD_DEST
- Binary:          $BINARY_DEST

EOF

    if [[ "$PURGE_MODE" == "true" ]]; then
        cat <<EOF
- Configuration:   $CONFIG_DIR
- State data:      $STATE_DIR

EOF
    else
        cat <<EOF
${YELLOW}Preserved:${NC}
- Configuration:   $CONFIG_DIR
- State data:      $STATE_DIR

To completely remove all data, run: sudo bash uninstall.sh --purge

EOF
    fi
}

# Main uninstallation flow
main() {
    log_info "Starting Conspiracy mesh network daemon uninstallation"
    
    check_root
    
    stop_and_disable_service
    remove_systemd_unit
    remove_binary
    remove_config_and_state
    
    print_summary
}

# Run main function
main
