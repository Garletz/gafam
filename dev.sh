#!/bin/bash

# GAFAM Unified Development Launcher
# Runs the Loco Rust backend and Vite Svelte frontend simultaneously
# Cleanly intercepts Ctrl+C (SIGINT) to kill both processes immediately without locking ports.

BACKEND_DIR="backend"
FRONTEND_DIR="frontend"

echo "========================================="
echo "   GAFAM UNIFIED LAUNCHER [LOCAL DEV]    "
echo "========================================="

# Cleanup function to kill background processes cleanly
cleanup() {
    echo -e "\n\n[GAFAM] Shutting down both nodes cleanly..."
    
    if [ ! -z "$BACKEND_PID" ]; then
        echo "[GAFAM] Terminating Loco Backend (PID: $BACKEND_PID)..."
        kill -15 "$BACKEND_PID" 2>/dev/null || kill -9 "$BACKEND_PID" 2>/dev/null
    fi
    
    if [ ! -z "$FRONTEND_PID" ]; then
        echo "[GAFAM] Terminating Svelte Frontend (PID: $FRONTEND_PID)..."
        kill -15 "$FRONTEND_PID" 2>/dev/null || kill -9 "$FRONTEND_PID" 2>/dev/null
    fi

    echo "[GAFAM] All nodes closed cleanly. Bye!"
    exit 0
}

# Capture Ctrl+C, kill signals, and exit triggers
trap cleanup SIGINT SIGTERM EXIT

# 1. Start the Rust Loco backend
echo -e "\n[GAFAM] Starting Loco Backend (Rust)..."
cd "$BACKEND_DIR" || exit 1
cargo run -- start &
BACKEND_PID=$!
cd ..

# Allow the Rust server to bind to port 5150 before launching Vite
sleep 1.8

# 2. Start the Vite Svelte frontend
echo -e "\n[GAFAM] Starting Spatial Frontend (Vite/Svelte)..."
cd "$FRONTEND_DIR" || exit 1
npm run dev &
FRONTEND_PID=$!
cd ..

echo -e "\n========================================="
echo " GAFAM POC ACTIVE AND STREAMING!"
echo " 🌐 Frontend Deck:  http://localhost:5173"
echo " ⚙️  Backend Server: http://localhost:5150"
echo " 👉 Press Ctrl+C in this window to stop both cleanly."
echo "=========================================\n"

# Maintain active state to await process termination
wait
