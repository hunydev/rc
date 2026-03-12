#!/usr/bin/env bash
#
# copilot-web 서비스 관리 스크립트
# 사용법: ./service.sh {install|uninstall|start|stop|restart|status|build|logs}
#
set -euo pipefail

SERVICE_NAME="rc"
PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="${PROJECT_DIR}/${SERVICE_NAME}"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

# 기본 실행 옵션 (환경변수로 오버라이드 가능)
PORT="${RC_PORT:-8000}"
COMMAND="${RC_COMMAND:-copilot --yolo}"
COLS="${RC_COLS:-120}"
ROWS="${RC_ROWS:-30}"

USER="$(whoami)"
GROUP="$(id -gn)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }

# ───────────────────────────── install ─────────────────────────────
do_install() {
    info "systemd 서비스 등록 중..."

    if [[ ! -f "$BINARY" ]]; then
        warn "바이너리가 없습니다. 먼저 빌드합니다."
        do_build_only
    fi

    sudo tee "$SERVICE_FILE" > /dev/null <<EOF
[Unit]
Description=rc - Remote Control terminal via browser
After=network.target

[Service]
Type=simple
User=${USER}
Group=${GROUP}
WorkingDirectory=${PROJECT_DIR}
ExecStart=${BINARY} --port ${PORT} --command "${COMMAND}" --cols ${COLS} --rows ${ROWS}
Restart=on-failure
RestartSec=3
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

# 환경
Environment=HOME=/home/${USER}
Environment=PATH=/usr/local/bin:/usr/bin:/bin:/home/${USER}/.local/bin:/home/${USER}/go/bin

# 보안 강화
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=${PROJECT_DIR}
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable "${SERVICE_NAME}"
    info "서비스 등록 완료: ${SERVICE_FILE}"
    info "시작하려면: ./service.sh start"
}

# ───────────────────────────── uninstall ───────────────────────────
do_uninstall() {
    info "systemd 서비스 해제 중..."

    if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
        sudo systemctl stop "${SERVICE_NAME}"
        info "서비스 중지됨"
    fi

    if [[ -f "$SERVICE_FILE" ]]; then
        sudo systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
        sudo rm -f "$SERVICE_FILE"
        sudo systemctl daemon-reload
        info "서비스 해제 완료"
    else
        warn "서비스 파일이 존재하지 않습니다: ${SERVICE_FILE}"
    fi
}

# ───────────────────────────── start / stop / restart / status ─────
do_start() {
    sudo systemctl start "${SERVICE_NAME}"
    info "서비스 시작됨 (port ${PORT})"
    sleep 1
    do_status
}

do_stop() {
    sudo systemctl stop "${SERVICE_NAME}"
    info "서비스 중지됨"
}

do_restart() {
    sudo systemctl restart "${SERVICE_NAME}"
    info "서비스 재시작됨"
    sleep 1
    do_status
}

do_status() {
    echo ""
    systemctl status "${SERVICE_NAME}" --no-pager || true
    echo ""
    # health check
    if curl -sf "http://localhost:${PORT}/health" > /dev/null 2>&1; then
        local health
        health=$(curl -sf "http://localhost:${PORT}/health")
        info "Health: ${health}"
    else
        warn "Health endpoint 응답 없음 (아직 기동 중이거나 중지 상태)"
    fi
}

# ───────────────────────────── build only (내부용) ─────────────────
do_build_only() {
    info "빌드 중... (go build)"
    cd "$PROJECT_DIR"
    go build -o "${SERVICE_NAME}" .
    info "빌드 완료: ${BINARY}"
}

# ───────────────────────────── build (stop → build → start) ────────
do_build() {
    local was_running=false

    if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
        was_running=true
        info "서비스 중지 중..."
        sudo systemctl stop "${SERVICE_NAME}"
        # 프로세스가 완전히 종료될 때까지 대기
        sleep 1
        info "서비스 중지 완료"
    fi

    do_build_only

    if [[ "$was_running" == true ]]; then
        info "서비스 재시작 중..."
        sudo systemctl start "${SERVICE_NAME}"
        sleep 1
        info "서비스 재시작 완료"
        do_status
    else
        warn "서비스가 실행 중이 아니었으므로 빌드만 수행했습니다."
        info "시작하려면: ./service.sh start"
    fi
}

# ───────────────────────────── logs ────────────────────────────────
do_logs() {
    local lines="${1:-50}"
    info "최근 로그 ${lines}줄:"
    echo ""
    journalctl -u "${SERVICE_NAME}" --no-pager -n "${lines}"
}

do_logs_follow() {
    info "로그 실시간 추적 (Ctrl+C로 종료):"
    journalctl -u "${SERVICE_NAME}" -f
}

# ───────────────────────────── usage ───────────────────────────────
usage() {
    cat <<EOF

${CYAN}╔══════════════════════════════════════════════════════════════╗
║               rc 서비스 관리 스크립트                        ║
╚══════════════════════════════════════════════════════════════╝${NC}

${GREEN}사용법:${NC}  ./service.sh <명령> [옵션]

${YELLOW}서비스 관리:${NC}
  install       systemd 서비스 등록 (바이너리 없으면 자동 빌드)
  uninstall     systemd 서비스 해제 (중지 + 삭제)
  start         서비스 시작
  stop          서비스 중지
  restart       서비스 재시작
  status        서비스 상태 + health check

${YELLOW}빌드:${NC}
  build         서비스 중지 → 빌드 → 서비스 재시작 (자동)

${YELLOW}로그:${NC}
  logs [N]      최근 N줄 로그 출력 (기본 50줄)
  logs-follow   실시간 로그 추적 (tail -f)

${YELLOW}환경변수 (install 전에 설정):${NC}
  RC_PORT               포트 번호    (기본: 8000)
  RC_COMMAND            실행 명령    (기본: copilot --yolo)
  RC_COLS               터미널 컬럼  (기본: 120)
  RC_ROWS               터미널 행    (기본: 30)

${YELLOW}사용 예시:${NC}
  ./service.sh install                 # 서비스 등록
  ./service.sh start                   # 서비스 시작
  ./service.sh build                   # 중지 → 빌드 → 재시작
  ./service.sh logs 100                # 최근 100줄 로그
  ./service.sh uninstall               # 서비스 해제

  # 포트 변경하여 등록
  RC_PORT=9000 ./service.sh install

EOF
}

# ───────────────────────────── main ────────────────────────────────
case "${1:-}" in
    install)     do_install ;;
    uninstall)   do_uninstall ;;
    start)       do_start ;;
    stop)        do_stop ;;
    restart)     do_restart ;;
    status)      do_status ;;
    build)       do_build ;;
    logs)        do_logs "${2:-50}" ;;
    logs-follow) do_logs_follow ;;
    *)           usage ;;
esac
