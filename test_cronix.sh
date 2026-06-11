#!/bin/bash
cd /mnt/d/claudeprj/codex
rm -f cronix
go build -o cronix .
./cronix serve > cronix.log 2>&1 &
CRONIX_PID=$!
sleep 5
curl -s http://localhost:8080/login | wc -c
kill $CRONIX_PID
