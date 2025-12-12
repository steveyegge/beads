#!/bin/bash
# Beads Event Log Viewer
# Pretty-print and filter events from .beads/events.log

EVENT_LOG=".beads/events.log"

# Check if event log exists
if [ ! -f "$EVENT_LOG" ]; then
    echo "Error: No event log found at $EVENT_LOG"
    exit 1
fi

# Parse command line arguments
case "$1" in
    --session)
        if [ -z "$2" ]; then
            echo "Usage: $0 --session SESSION_ID"
            exit 1
        fi
        echo "Events for session $2:"
        echo "════════════════════════════════════════════════════════════"
        grep "|$2|" "$EVENT_LOG"
        ;;

    --issue)
        if [ -z "$2" ]; then
            echo "Usage: $0 --issue ISSUE_ID"
            exit 1
        fi
        echo "Events for issue $2:"
        echo "════════════════════════════════════════════════════════════"
        grep "|$2|" "$EVENT_LOG"
        ;;

    --category)
        if [ -z "$2" ]; then
            echo "Usage: $0 --category PREFIX"
            exit 1
        fi
        echo "Events in category $2.*:"
        echo "════════════════════════════════════════════════════════════"
        grep "|$2\." "$EVENT_LOG"
        ;;

    --summary)
        echo "Event Summary:"
        echo "════════════════════════════════════════════════════════════"
        cut -d'|' -f2 "$EVENT_LOG" | sort | uniq -c | sort -rn
        ;;

    --last)
        N="${2:-10}"
        echo "Last $N events:"
        echo "════════════════════════════════════════════════════════════"
        tail -n "$N" "$EVENT_LOG"
        ;;

    --help)
        cat <<EOF
Beads Event Log Viewer

Usage: $0 [OPTIONS]

OPTIONS:
    --session ID        Show all events for a specific session ID
    --issue ID          Show all events for a specific issue ID
    --category PREFIX   Show all events matching category prefix (e.g., 'sk', 'hk', 'bd')
    --summary           Show event count summary by type
    --last [N]          Show last N events (default: 10)
    --help              Show this help message

Event Log Format:
    TIMESTAMP|EVENT_CODE|ISSUE_ID|AGENT_ID|SESSION_ID|DETAILS

Event Categories:
    ep.*    Epoch (application lifecycle)
    ss.*    Session (agent session lifecycle)
    sk.*    Skill (Claude skill activations)
    bd.*    Beads (issue tracker operations)
    gt.*    Git (version control operations)
    hk.*    Hook (git hook triggers)
    gd.*    Guard (enforcement/constraint checks)

Examples:
    $0 --last 20
    $0 --category sk
    $0 --issue bd-abc123
    $0 --summary

EOF
        ;;

    *)
        echo "Usage: $0 [--session ID] [--issue ID] [--category PREFIX] [--summary] [--last N] [--help]"
        echo "Run '$0 --help' for detailed usage information"
        exit 1
        ;;
esac
