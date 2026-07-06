#!/bin/bash
# Test WebSocket connection to VPS port 5150
# This mimics what the Rust bridge does

VPS_IP="165.245.249.166"
PORT="5150"

echo "=== Testing connectivity to VPS ==="
echo ""

# Test 1: TCP connection
echo "1. TCP connection to $VPS_IP:$PORT..."
nc -z -w 3 $VPS_IP $PORT 2>&1
if [ $? -eq 0 ]; then
    echo "   ✅ TCP OK"
else
    echo "   ❌ TCP FAILED - Port not reachable!"
    exit 1
fi

# Test 2: HTTP ping
echo "2. HTTP /api/_ping..."
PING=$(curl -s -m 5 "http://$VPS_IP:$PORT/api/_ping")
echo "   Response: $PING"

# Test 3: WebSocket upgrade without auth
echo "3. WebSocket upgrade (no auth)..."
WS_NOAUTH=$(curl -s -m 5 -o /dev/null -w "%{http_code}" \
    -H "Connection: Upgrade" \
    -H "Upgrade: websocket" \
    -H "Sec-WebSocket-Version: 13" \
    -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
    "http://$VPS_IP:$PORT/ws/scrcpy/bridge")
echo "   HTTP Status: $WS_NOAUTH (expect 401)"

# Test 4: Check if scrcpy routes exist on VPS  
echo "4. Check scrcpy status endpoint..."
STATUS=$(curl -s -m 5 "http://$VPS_IP:$PORT/api/scrcpy/status")
echo "   Response: $STATUS"

# Test 5: Check Docker container logs on VPS (via the HTTPS port with the actual JWT)
echo ""
echo "=== VPS is reachable. The problem is likely: ==="
echo "  a) The JWT token used by Tauri doesn't match the VPS JWT_SECRET"
echo "  b) The scrcpy-server fails to start on the phone"
echo "  c) The scrcpy tunnel (127.0.0.1:27183) fails to connect"
echo ""
echo "Check: Does your phone have USB Debugging enabled?"
echo "Check: Is ADB authorized on the phone?"

# Test 6: Check ADB
echo ""
echo "=== ADB Status ==="
ADB_PATH="$HOME/Library/Android/sdk/platform-tools/adb"
if [ -f "$ADB_PATH" ]; then
    echo "ADB found at: $ADB_PATH"
    $ADB_PATH devices -l 2>&1
else
    echo "ADB not found at default SDK location, trying PATH..."
    adb devices -l 2>&1
fi
