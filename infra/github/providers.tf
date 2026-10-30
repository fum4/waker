# Auth: GITHUB_TOKEN, exported in memory by `mise run tf:github` from the devbox's
# own `gh` identity (`gh auth token`, scope `repo`) — no secret of this repo's.
provider "github" {
  owner = "fum4"
}
