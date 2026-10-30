# The merge gate (devbox docs/ci.md § Public repos): waker is public, so its CI is
# GitHub Actions and `wt merge` reads this ruleset's required checks.
locals {
  # The job names of .github/workflows/ci.yml. Renaming a job there is a change
  # here in the same PR, or every PR waits on a check that never reports.
  required_checks = ["lint", "test", "e2e", "build"]

  # The GitHub Actions app: a check counts only when Actions reports it, not a
  # commit status anyone with write access could post under the same name.
  github_actions_app_id = 15368
}

resource "github_repository_ruleset" "default_branch" {
  name        = "default-branch"
  repository  = "waker"
  target      = "branch"
  enforcement = "active"

  conditions {
    ref_name {
      include = ["~DEFAULT_BRANCH"]
      exclude = []
    }
  }

  # No bypass actors, the repo admin included: the owner's web-UI merge is gated by
  # the same checks as `wt merge`. Recovery (a force-push, a stuck check) is an
  # apply with enforcement = "disabled", then back to "active".

  rules {
    # Every change is a PR. 0 approvals: agents and the owner are one account, and
    # GitHub never lets an author approve their own PR.
    pull_request {
      required_approving_review_count   = 0
      dismiss_stale_reviews_on_push     = false
      require_code_owner_review         = false
      require_last_push_approval        = false
      required_review_thread_resolution = false
    }

    # Not strict: `wt pr` already rebases, and strict mode re-runs every check on
    # every PR after each merge.
    required_status_checks {
      strict_required_status_checks_policy = false

      dynamic "required_check" {
        for_each = local.required_checks
        content {
          context        = required_check.value
          integration_id = local.github_actions_app_id
        }
      }
    }

    deletion         = true
    non_fast_forward = true
  }
}
