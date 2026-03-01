@echo off
if "%1"=="onboard" (
    python onboard.py %*
) else (
    echo Usage: moticlaw onboard
)
