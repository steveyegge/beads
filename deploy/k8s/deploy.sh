#!/usr/bin/env bash
set -euo pipefail

# Deploy bd-slack-bot to Kubernetes (gastown namespace)
#
# Prerequisites:
#   1. kubectl configured with cluster access
#   2. Docker image pushed to ghcr.io/groblegark/beads
#   3. Slack bot and app tokens ready
#
# Usage:
#   ./deploy.sh                          # Apply all manifests
#   ./deploy.sh --set-secrets            # Create secrets interactively
#   ./deploy.sh --set-channel C0123456   # Set default channel
#   ./deploy.sh --status                 # Show deployment status
#   ./deploy.sh --logs                   # Tail slack-bot logs
#   ./deploy.sh --restart                # Rolling restart

NAMESPACE="gastown"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --set-secrets         Set Slack bot/app tokens interactively"
    echo "  --set-channel ID      Set default Slack channel ID"
    echo "  --status              Show deployment status"
    echo "  --logs                Tail slack-bot container logs"
    echo "  --logs-daemon         Tail bd-daemon container logs"
    echo "  --restart             Rolling restart of the deployment"
    echo "  --delete              Delete all resources"
    echo "  -h, --help            Show this help"
    echo ""
    echo "With no options: applies all K8s manifests (PVC, config, deployment)."
}

ensure_namespace() {
    if ! kubectl get namespace "$NAMESPACE" &>/dev/null; then
        echo "Creating namespace $NAMESPACE..."
        kubectl create namespace "$NAMESPACE"
    fi
}

set_secrets() {
    ensure_namespace
    echo "Setting Slack credentials for namespace $NAMESPACE"
    read -rsp "Bot token (xoxb-...): " BOT_TOKEN
    echo
    read -rsp "App token (xapp-...): " APP_TOKEN
    echo

    kubectl create secret generic slack-credentials \
        --namespace="$NAMESPACE" \
        --from-literal=bot-token="$BOT_TOKEN" \
        --from-literal=app-token="$APP_TOKEN" \
        --dry-run=client -o yaml | kubectl apply -f -

    echo "Secrets updated."
}

set_channel() {
    local channel_id="$1"
    ensure_namespace
    kubectl create configmap slack-config \
        --namespace="$NAMESPACE" \
        --from-literal=default-channel="$channel_id" \
        --dry-run=client -o yaml | kubectl apply -f -
    echo "Default channel set to $channel_id"
}

apply_manifests() {
    ensure_namespace
    echo "Applying K8s manifests to namespace $NAMESPACE..."
    kubectl apply -f "$SCRIPT_DIR/slack-pvc.yaml"
    kubectl apply -f "$SCRIPT_DIR/slack-config.yaml"
    kubectl apply -f "$SCRIPT_DIR/slack-bot.yaml"
    echo ""
    echo "Deployment applied. Check status with: $0 --status"
    echo ""
    echo "Next steps:"
    echo "  1. Set Slack tokens:    $0 --set-secrets"
    echo "  2. Set channel:         $0 --set-channel C0123456789"
    echo "  3. Set up GHCR pull:    kubectl create secret docker-registry ghcr-credentials \\"
    echo "                            --namespace=$NAMESPACE \\"
    echo "                            --docker-server=ghcr.io \\"
    echo "                            --docker-username=YOUR_GITHUB_USER \\"
    echo "                            --docker-password=YOUR_GITHUB_PAT"
}

show_status() {
    echo "=== Deployment ==="
    kubectl get deployment bd-slack-bot -n "$NAMESPACE" -o wide 2>/dev/null || echo "Not found"
    echo ""
    echo "=== Pods ==="
    kubectl get pods -n "$NAMESPACE" -l app=beads,component=slack-bot -o wide 2>/dev/null || echo "No pods"
    echo ""
    echo "=== Service ==="
    kubectl get service bd-slack-bot -n "$NAMESPACE" 2>/dev/null || echo "Not found"
    echo ""
    echo "=== Events (last 10) ==="
    kubectl get events -n "$NAMESPACE" --sort-by='.lastTimestamp' --field-selector involvedObject.name=bd-slack-bot 2>/dev/null | tail -10 || true
}

tail_logs() {
    local container="${1:-slack-bot}"
    kubectl logs -f -n "$NAMESPACE" -l app=beads,component=slack-bot -c "$container"
}

restart() {
    kubectl rollout restart deployment/bd-slack-bot -n "$NAMESPACE"
    echo "Rolling restart initiated. Watching..."
    kubectl rollout status deployment/bd-slack-bot -n "$NAMESPACE" --timeout=120s
}

delete_all() {
    echo "Deleting all bd-slack-bot resources from namespace $NAMESPACE..."
    kubectl delete -f "$SCRIPT_DIR/slack-bot.yaml" --ignore-not-found
    kubectl delete -f "$SCRIPT_DIR/slack-config.yaml" --ignore-not-found
    echo "Kept PVC (beads-data) — delete manually if needed:"
    echo "  kubectl delete -f $SCRIPT_DIR/slack-pvc.yaml"
}

# Parse arguments
if [[ $# -eq 0 ]]; then
    apply_manifests
    exit 0
fi

case "$1" in
    --set-secrets)
        set_secrets
        ;;
    --set-channel)
        [[ -z "${2:-}" ]] && { echo "Error: channel ID required"; exit 1; }
        set_channel "$2"
        ;;
    --status)
        show_status
        ;;
    --logs)
        tail_logs "slack-bot"
        ;;
    --logs-daemon)
        tail_logs "bd-daemon"
        ;;
    --restart)
        restart
        ;;
    --delete)
        delete_all
        ;;
    -h|--help)
        usage
        ;;
    *)
        echo "Unknown option: $1"
        usage
        exit 1
        ;;
esac
