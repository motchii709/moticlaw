@echo off
if "%1"=="onboard" (
    python onboard.py onboard
) else (
    echo Usage: moticlaw onboard
)
