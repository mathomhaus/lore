# Deploy to production

Runbook for deploying lore consumers to production.

## Prerequisites

- Go 1.23+ toolchain installed
- Access to the release repository
- Signing key available in the CI environment

## Steps

1. Tag the commit: `git tag v0.x.y`
2. Push the tag: `git push upstream v0.x.y`
3. Wait for the release workflow to complete.
4. Verify the release assets are present on the GitHub releases page.
5. Update the brew tap SHA if applicable.

## Rollback

If the release is broken, delete the tag and re-push after the fix:

```bash
git tag -d v0.x.y
git push upstream :refs/tags/v0.x.y
```
