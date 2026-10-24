#!/usr/bin/env bash
# main-ci-alert.sh: surface a red main (.github/workflows/main-ci-alert.yml;
# engdocs/CI_REQUIRED_CHECK_TOPOLOGY.md, "Merge Queue").
#
# A merge group runs the fast set only: the Windows, macOS and Nix jobs and
# the Dolt-tier Bazel lanes first run on main's push, after the merge
# (pr.yml's and pr-risk.yml's push runs, bazel.yml's own push run). This runs
# once per completed push run on main of a tracked workflow:
#
#   failure, timed_out, startup_failure
#       open the ci/main-red issue with the workflow, the run, the commit, the
#       PR its squash subject names ("... (#N)") and the failed jobs; while
#       one is open, comment on it instead (one issue at a time). Comment on
#       that PR too.
#   success
#       with the issue open: comment when this workflow's previous decided
#       push run was red, listing what is still red, and close the issue
#       once no tracked workflow's latest decided push run on main is red.
#   anything else (cancelled, skipped, ...)
#       nothing: it says nothing about the tree.
#
# A decided run is one that succeeded or went red: a cancelled run (main.yml
# cancels its older runs) is passed over. This run counts as its workflow's
# latest unless the runs API lists a newer one.
#
# Env: GITHUB_REPOSITORY; GH_TOKEN (issues: write, pull-requests: write,
# actions: read); the triggering run's WORKFLOW_NAME, RUN_ID, RUN_URL,
# RUN_CREATED_AT, CONCLUSION, HEAD_SHA and HEAD_COMMIT_MESSAGE.

set -euo pipefail
export LC_ALL=C

repo=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is unset}
label="ci/main-red"
title="CI is red on main"

# The tracked workflows, by the name workflow_run matches, and their files
# (main-ci-alert.yml's workflow_run list names the same four).
names=("PR" "PR Risk" "Bazel" "Main")
files=(pr.yml pr-risk.yml bazel.yml main.yml)
tracked=$(printf '%s, ' "${names[@]}")
tracked=${tracked%, }

workflow=${WORKFLOW_NAME:?WORKFLOW_NAME is unset}
file=
for i in "${!names[@]}"; do
	if [[ "${names[$i]}" == "$workflow" ]]; then
		file=${files[$i]}
	fi
done
[[ -n "$file" ]] || {
	echo "::error::main-ci-alert.sh: '$workflow' is not a tracked workflow ($tracked)"
	exit 1
}
[[ "${RUN_ID:-}" =~ ^[0-9]+$ ]] || {
	echo "::error::main-ci-alert.sh: RUN_ID '${RUN_ID:-}' is not a run id"
	exit 1
}
[[ "${RUN_CREATED_AT:-}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] || {
	echo "::error::main-ci-alert.sh: RUN_CREATED_AT '${RUN_CREATED_AT:-}' is not a UTC timestamp"
	exit 1
}
run_url=${RUN_URL:?RUN_URL is unset}
sha=${HEAD_SHA:?HEAD_SHA is unset}
conclusion=${CONCLUSION:?CONCLUSION is unset}

is_red() {
	case "$1" in
		failure | timed_out | startup_failure) return 0 ;;
	esac
	return 1
}

# decided_runs FILE: the workflow's decided push runs on main, newest first,
# one "created_at conclusion html_url" line each.
decided_runs() {
	gh api "repos/$repo/actions/workflows/$1/runs?branch=main&event=push&status=completed&per_page=30&exclude_pull_requests=true" |
		jq -r '.workflow_runs[]
			| select(.conclusion == "success" or .conclusion == "failure" or .conclusion == "timed_out" or .conclusion == "startup_failure")
			| "\(.created_at) \(.conclusion) \(.html_url)"'
}

open_issue() {
	gh issue list --repo "$repo" --label "$label" --state open --json number --jq '.[0].number // empty'
}

case "$conclusion" in
	failure | timed_out | startup_failure | success) ;;
	*)
		echo "$workflow concluded $conclusion on main at $sha: nothing to report"
		exit 0
		;;
esac

body=$(mktemp)
trap 'rm -f "$body"' EXIT
issue=$(open_issue)

if [[ "$conclusion" != success ]]; then
	# The commit subject, inert in Markdown: an inline code span, so it can
	# neither mention anyone nor format; backticks would end the span.
	subject=${HEAD_COMMIT_MESSAGE-}
	subject=${subject%%$'\n'*}
	subject=${subject//\`/\'}
	pr=
	if [[ "$subject" =~ \(#([0-9]+)\)$ ]]; then
		pr=${BASH_REMATCH[1]}
	fi
	jobs=$(gh api --paginate "repos/$repo/actions/runs/$RUN_ID/jobs?per_page=100" |
		jq -r '.jobs[] | select(.conclusion == "failure" or .conclusion == "timed_out" or .conclusion == "cancelled") | "- [\(.name)](\(.html_url)): \(.conclusion)"')
	{
		printf '**%s** is %s on main at %s: %s\n\n' "$workflow" "$conclusion" "$sha" "$run_url"
		# shellcheck disable=SC2016 # Markdown backticks, not a command
		printf 'Commit: `%s`\n' "$subject"
		if [[ -n "$pr" ]]; then
			printf 'Landed by: #%s\n\n' "$pr"
		else
			printf 'Landed by: no PR number in the commit subject (a direct push?)\n\n'
		fi
		if [[ -n "$jobs" ]]; then
			printf 'Failed jobs:\n%s\n' "$jobs"
		else
			printf 'Failed jobs: none listed (no job ran: a startup failure, or the run was cut short).\n'
		fi
		if [[ -z "$issue" ]]; then
			printf '\nThe merge queue tested this commit on the fast set only; its push runs are the first run of the rest (Windows, macOS, Nix, the Dolt-tier Bazel lanes). Fix forward or revert. Later reds comment here, and the issue closes itself once no tracked workflow'"'"'s latest push run on main (%s) is red (engdocs/CI_REQUIRED_CHECK_TOPOLOGY.md, "Merge Queue").\n' "$tracked"
		fi
	} >"$body"
	if [[ -n "$issue" ]]; then
		gh issue comment "$issue" --repo "$repo" --body-file "$body"
	else
		# The label may not exist yet; an existing one is left as it is.
		gh label create "$label" --repo "$repo" --color b60205 \
			--description "A push run on main is red (main-ci-alert.yml)" >/dev/null 2>&1 || true
		url=$(gh issue create --repo "$repo" --title "$title" --label "$label" --body-file "$body")
		issue=${url##*/}
	fi
	echo "$workflow is $conclusion on main at $sha: reported on #$issue"
	if [[ -n "$pr" ]]; then
		printf 'The commit this PR landed on main (%s) is %s in **%s**: %s\n\nTracked in #%s.\n' \
			"$sha" "$conclusion" "$workflow" "$run_url" "$issue" >"$body"
		gh pr comment "$pr" --repo "$repo" --body-file "$body" ||
			echo "::warning::main-ci-alert.sh: could not comment on #$pr; #$issue has the report"
	fi
	exit 0
fi

# success
if [[ -z "$issue" ]]; then
	echo "$workflow is green on main at $sha; no ci/main-red issue is open"
	exit 0
fi
# This workflow's previous decided run, and every tracked workflow's latest
# (this run when the API lists nothing newer of its workflow). Each list is
# read whole first: an early-exiting reader (head, awk's exit) could cut its
# writer off and, under pipefail, end this script.
previous=
red=()
for i in "${!names[@]}"; do
	runs=$(decided_runs "${files[$i]}")
	if [[ "${names[$i]}" == "$workflow" ]]; then
		while read -r created run_conclusion _; do
			if [[ -n "$created" && "$created" < "$RUN_CREATED_AT" ]]; then
				previous=$run_conclusion
				break
			fi
		done <<<"$runs"
	fi
	read -r created latest_conclusion latest_url <<<"${runs%%$'\n'*}" || true
	if [[ "${names[$i]}" == "$workflow" && ! "${created:-}" > "$RUN_CREATED_AT" ]]; then
		continue
	fi
	if is_red "${latest_conclusion:-}"; then
		red+=("${names[$i]} ($latest_url)")
	fi
done
if is_red "${previous:-}"; then
	{
		printf '**%s** is green again on main at %s: %s\n' "$workflow" "$sha" "$run_url"
		if ((${#red[@]} > 0)); then
			still=$(printf '%s, ' "${red[@]}")
			printf '\nStill red: %s\n' "${still%, }"
		fi
	} >"$body"
	gh issue comment "$issue" --repo "$repo" --body-file "$body"
fi
if ((${#red[@]} == 0)); then
	gh issue close "$issue" --repo "$repo" --comment "No tracked workflow's latest push run on main ($tracked) is red: closing. $workflow is green at $sha: $run_url"
	echo "closed #$issue"
else
	echo "#$issue stays open: ${red[*]}"
fi
