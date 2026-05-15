# Integration Testing Guide

This guide explains how to run integration tests for the Conspiracy mesh networking platform.

## Overview

The integration tests validate end-to-end mesh behavior using a 3-node topology:
- **Node A**: Bootstrap node (initial BEACON transmitter)
- **Node B**: Intermediate node (joins A, forwards traffic)
- **Node C**: Leaf node (joins B, can reach A through B)

The tests verify:
1. BEACON transmission and reception
2. JOIN_REQ/JOIN_ACK handshake
3. Multi-hop packet forwarding (A → B → C)
4. Mesh connectivity after all nodes join

## Running Tests Locally

### Prerequisites

- Go ≥ 1.22
- Docker and Docker Compose (optional, for isolated testing)

### Method 1: Direct Go Test Execution

Run integration tests with the `integration` build tag:

```bash
go test -v -tags=integration -timeout=10m ./test/integration
```

This will:
1. Create 3 simulated nodes using UDP radios (localhost:10001, 10002, 10003)
2. Initialize crypto components (reboot counter, nonce generator)
3. Execute the 4-phase test sequence:
   - Phase 1: Node A starts sending BEACONs
   - Phase 2: Node B scans and joins Node A
   - Phase 3: Node C scans and joins Node B
   - Phase 4: Verify all nodes can exchange BEACONs

Expected duration: ~5 minutes

### Method 2: Docker Compose (Isolated Environment)

For fully isolated testing with persistent storage:

```bash
cd test/integration
docker-compose up --build
```

This creates:
- 3 separate containers (node-a, node-b, node-c)
- Isolated network (172.20.0.0/24)
- Persistent volumes for each node's storage

To clean up after tests:
```bash
docker-compose down -v
```

## Test Structure

### Test Phases

| Phase | Description | Duration | Success Criteria |
|-------|-------------|----------|------------------|
| 1 | Node A Bootstrap | ~10s | Node A transmits initial BEACON |
| 2 | Node B Join A | ~90s | Node B FSM reaches CONNECTED state |
| 3 | Node C Join B | ~90s | Node C FSM reaches CONNECTED state |
| 4 | Verify Connectivity | ~30s | Each node receives BEACONs from neighbors |

### UDP Radio Simulation

The tests use `UDPRadio` (from `internal/lora/udp_radio.go`) instead of real LoRa hardware:
- **Advantages**: No hardware required, deterministic timing, CI-friendly
- **Limitations**: Does not validate RF propagation, duty-cycle enforcement, or RSSI accuracy

For real-world RF validation, see `docs/hardware-testing.md`.

## Expected Test Output

Successful test run:
```
=== RUN   TestThreeNodeMesh
=== RUN   TestThreeNodeMesh/Phase1_NodeA_Bootstrap
    three_node_test.go:78: Node A: Starting BEACON transmission...
    three_node_test.go:84: Node A: Successfully transmitted BEACON
--- PASS: TestThreeNodeMesh/Phase1_NodeA_Bootstrap (0.05s)
=== RUN   TestThreeNodeMesh/Phase2_NodeB_JoinA
    three_node_test.go:89: Node B: Starting auto-join FSM to discover Node A...
    three_node_test.go:116: Node B: Successfully joined Node A
--- PASS: TestThreeNodeMesh/Phase2_NodeB_JoinA (32.45s)
=== RUN   TestThreeNodeMesh/Phase3_NodeC_JoinB
    three_node_test.go:121: Node C: Starting auto-join FSM to discover Node B...
    three_node_test.go:142: Node C: Successfully joined Node B
--- PASS: TestThreeNodeMesh/Phase3_NodeC_JoinB (34.12s)
=== RUN   TestThreeNodeMesh/Phase4_VerifyMeshConnectivity
    three_node_test.go:148: Verifying mesh connectivity: A ↔ B ↔ C
    three_node_test.go:152: Node A: Successfully received 2 BEACONs
    three_node_test.go:152: Node B: Successfully received 2 BEACONs
    three_node_test.go:152: Node C: Successfully received 1 BEACONs
--- PASS: TestThreeNodeMesh/Phase4_VerifyMeshConnectivity (15.23s)
--- PASS: TestThreeNodeMesh (81.85s)
PASS
ok      github.com/opd-ai/conspiracy/test/integration  82.012s
```

## Troubleshooting

### Test hangs during Phase 2 or Phase 3
- **Cause**: FSM not receiving BEACONs within scan duration
- **Fix**: Check that Node A (or Node B) is transmitting periodic BEACONs
- **Debug**: Add `slog.SetLogLoggerLevel(slog.LevelDebug)` to see BEACON reception logs

### Error: "Failed to create reboot counter"
- **Cause**: Insufficient write permissions to temp directory
- **Fix**: Ensure `$TMPDIR` (or `/tmp`) is writable
- **Workaround**: Set `CONSPIRACYD_STORAGE_DIR=/path/to/writable/dir`

### Error: "bind: address already in use"
- **Cause**: Previous test process still holding UDP ports 10001-10003
- **Fix**: Kill orphaned processes: `pkill -f "go test.*integration"`
- **Note**: Wait 1-2 seconds after kill for socket cleanup

### Phase 4 reports "Expected at least 1 BEACONs, received 0"
- **Cause**: BEACON transmission stopped prematurely or crypto decryption failing
- **Fix**: Verify all nodes use same `meshKey`
- **Debug**: Check for encryption/decryption errors in test logs

## CI Integration

### GitHub Actions Workflow

Add integration tests to `.github/workflows/ci.yml`:

```yaml
  integration-test:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4
    
    - name: Set up Go
      uses: actions/setup-go@v5
      with:
        go-version: '1.25'
    
    - name: Run integration tests
      run: go test -v -tags=integration -timeout=10m ./test/integration
```

### Docker Compose in CI

For Docker-based testing:

```yaml
  integration-test-docker:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4
    
    - name: Run integration tests with Docker Compose
      run: |
        cd test/integration
        docker-compose up --build --abort-on-container-exit
        docker-compose down -v
```

## Duty-Cycle Compliance Testing

### TestDutyCycleCompliance

This test validates regulatory compliance with EU/US LoRa duty-cycle limits by simulating a 100-node mesh over a 1-hour test period.

**Running the test:**

```bash
go test -v -tags=integration -timeout=70m ./test/integration -run TestDutyCycleCompliance
```

**Test parameters:**
- **Nodes**: 100 simulated nodes
- **Duration**: 1 hour
- **EU limit**: 1% duty-cycle (36 seconds/hour total ToA)
- **US limit**: 4% duty-cycle (144 seconds/hour total ToA)
- **Collision threshold**: <10%

**Acceptance criteria:**
- Aggregate duty-cycle must be below regional limit
- Collision rate must be <10%
- Adaptive BEACON intervals scale correctly with peer count

**Expected runtime**: ~65 minutes (1 hour test + setup/teardown)

**Note**: This is a long-running test. Use `-short` flag to skip:
```bash
go test -short -tags=integration ./test/integration
```

## Future Enhancements

Planned additions to integration test suite (Priority 7 roadmap):

- [ ] **5-node mesh**: Validate routing with 2 intermediate nodes (A → B → C → D → E)
- [ ] **Network partition**: Simulate split-brain and rejoin scenarios
- [ ] **ROUTE_HINT propagation**: Test batman-adv originator updates
- [x] **Duty-cycle enforcement**: Measure actual TX time across 1-hour test run
- [ ] **Key rotation**: Test REKEY frame propagation across mesh
- [ ] **Node failure recovery**: Kill intermediate node, verify route convergence

## References

- [Auto-Join FSM Design](../../docs/lora-mesh-design.md#§4.2)
- [UDP Radio Implementation](../../internal/lora/udp_radio.go)
- [Hardware Testing Guide](../../docs/hardware-testing.md)
