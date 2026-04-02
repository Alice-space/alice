#!/usr/bin/env bash
set -euo pipefail

PROGRAM="$(basename "$0")"
ALICE_HOME_DIR="${ALICE_HOME:-$HOME/.alice}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

for lib in \
  "$SCRIPT_DIR/lib/common.sh" \
  "$SCRIPT_DIR/lib/repo.sh" \
  "$SCRIPT_DIR/lib/bootstrap.sh" \
  "$SCRIPT_DIR/lib/commands.sh"
do
  [[ -f "$lib" ]] || {
    printf '[alice-code-army] ERROR: missing helper script: %s\n' "$lib" >&2
    exit 1
  }
  # shellcheck source=/dev/null
  source "$lib"
done

main() {
  local cmd="${1:-help}"
  case "$cmd" in
    help|-h|--help)
      usage
      ;;
    list|get)
      shift
      exec "$ALICE_BIN" runtime campaigns "$cmd" "$@"
      ;;
    delete)
      [[ $# -ge 2 && $# -le 3 ]] || die "usage: $PROGRAM delete CAMPAIGN_ID [--delete-repo]"
      shift
      exec "$ALICE_BIN" runtime campaigns delete "$@"
      ;;
    create)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM create CREATE_JSON"
      create_repo_first "$2"
      ;;
    bootstrap)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM bootstrap BOOTSTRAP_JSON"
      bootstrap_repo_first "$2"
      ;;
    init-repo)
      [[ $# -ge 2 && $# -le 3 ]] || die "usage: $PROGRAM init-repo CAMPAIGN_ID [DEST_DIR]"
      init_campaign_repo "$2" "${3:-}"
      ;;
    repo-scan)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM repo-scan CAMPAIGN_ID"
      run_campaigns repo-scan "$2"
      ;;
    repo-reconcile)
      [[ $# -ge 2 && $# -le 5 ]] || die "usage: $PROGRAM repo-reconcile CAMPAIGN_ID [--write-report=false] [--update-runtime=false] [--sync-dispatch=false]"
      shift
      run_campaigns repo-reconcile "$@"
      ;;
    repo-lint)
      [[ $# -ge 2 && $# -le 3 ]] || die "usage: $PROGRAM repo-lint CAMPAIGN_ID [--for-approval]"
      shift
      run_campaigns repo-lint "$@"
      ;;
    task-self-check)
      [[ $# -eq 4 ]] || die "usage: $PROGRAM task-self-check CAMPAIGN_ID TASK_ID executor|reviewer"
      shift
      run_campaigns task-self-check "$@"
      ;;
    task-guidance)
      [[ $# -eq 5 ]] || die "usage: $PROGRAM task-guidance CAMPAIGN_ID TASK_ID accept|resume GUIDANCE"
      shift
      run_campaigns task-guidance "$@"
      ;;
    plan-self-check)
      [[ $# -eq 4 ]] || die "usage: $PROGRAM plan-self-check CAMPAIGN_ID planner|planner_reviewer PLAN_ROUND"
      shift
      run_campaigns plan-self-check "$@"
      ;;
    patch)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM patch CAMPAIGN_ID PATCH_JSON"
      mutate_campaign_and_return "patch" "$2" "$3"
      ;;
    approve-plan)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM approve-plan CAMPAIGN_ID"
      approve_plan "$2"
      ;;
    plan-status)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM plan-status CAMPAIGN_ID"
      plan_status "$2"
      ;;
    apply-command)
      [[ $# -ge 3 && $# -le 4 ]] || die "usage: $PROGRAM apply-command CAMPAIGN_ID COMMAND [SOURCE]"
      apply_command "$2" "$3" "${4:-manual}"
      ;;
    *)
      die "unknown command: ${cmd}"
      ;;
  esac
}

main "$@"
