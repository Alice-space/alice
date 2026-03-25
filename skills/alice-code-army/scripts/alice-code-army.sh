#!/usr/bin/env bash
set -euo pipefail

PROGRAM="$(basename "$0")"
ALICE_HOME_DIR="${ALICE_HOME:-$HOME/.alice}"
DEFAULT_GITLAB_HOST="${ALICE_CODE_ARMY_GITLAB_HOST:-code.ihep.ac.cn}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

for lib in \
  "$SCRIPT_DIR/lib/common.sh" \
  "$SCRIPT_DIR/lib/repo.sh" \
  "$SCRIPT_DIR/lib/gitlab.sh" \
  "$SCRIPT_DIR/lib/render.sh" \
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
    create)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM create CREATE_JSON"
      create_repo_first "$2"
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
      [[ $# -ge 2 && $# -le 4 ]] || die "usage: $PROGRAM repo-reconcile CAMPAIGN_ID [--write-report=false] [--update-runtime=false]"
      shift
      run_campaigns repo-reconcile "$@"
      ;;
    patch)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM patch CAMPAIGN_ID PATCH_JSON"
      mutate_campaign_and_return "patch" "$2" "$3"
      ;;
    upsert-trial)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM upsert-trial CAMPAIGN_ID TRIAL_PAYLOAD_JSON"
      upsert_trial_and_return "$2" "$3"
      ;;
    add-guidance)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM add-guidance CAMPAIGN_ID GUIDANCE_PAYLOAD_JSON"
      mutate_campaign_and_return "add-guidance" "$2" "$3"
      ;;
    add-review)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM add-review CAMPAIGN_ID REVIEW_PAYLOAD_JSON"
      mutate_campaign_and_return "add-review" "$2" "$3"
      ;;
    add-pitfall)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM add-pitfall CAMPAIGN_ID PITFALL_PAYLOAD_JSON"
      mutate_campaign_and_return "add-pitfall" "$2" "$3"
      ;;
    render-issue-note)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM render-issue-note CAMPAIGN_ID"
      render_issue_note "$2"
      ;;
    render-trial-note)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM render-trial-note CAMPAIGN_ID TRIAL_ID"
      render_trial_note "$2" "$3"
      ;;
    time-stats)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM time-stats CAMPAIGN_ID"
      time_stats "$2"
      ;;
    time-estimate)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM time-estimate CAMPAIGN_ID DURATION"
      time_estimate "$2" "$3"
      ;;
    time-spend)
      [[ $# -ge 3 && $# -le 4 ]] || die "usage: $PROGRAM time-spend CAMPAIGN_ID DURATION [SUMMARY]"
      time_spend "$2" "$3" "${4:-}"
      ;;
    sync-issue)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM sync-issue CAMPAIGN_ID"
      sync_issue "$2"
      ;;
    sync-trial)
      [[ $# -eq 3 ]] || die "usage: $PROGRAM sync-trial CAMPAIGN_ID TRIAL_ID"
      sync_trial "$2" "$3"
      ;;
    sync-all)
      [[ $# -eq 2 ]] || die "usage: $PROGRAM sync-all CAMPAIGN_ID"
      sync_all "$2"
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
