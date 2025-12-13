@echo off
REM Open VS Code for beads development
REM Auto-runs "bd prime" on folder open via tasks.json

cd /d C:\myStuff\_infra\beads

REM Open VS Code in current directory
code .

echo VS Code opened. Task "Beads: Session Startup" will run automatically.
echo (Requires: Settings > task.allowAutomaticTasks = on)
