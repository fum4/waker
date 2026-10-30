output "ruleset_id" {
  description = "The default-branch ruleset's id (gh api repos/fum4/waker/rulesets/<id>)"
  value       = github_repository_ruleset.default_branch.ruleset_id
}
