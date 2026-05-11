# Upstream Sync Log

Tracks merges from upstream (`nextlevelbuilder/goclaw`) into this fork
(`x-project-coding/goclaw`). Append a new section per sync, newest at the top.

After each sync, walk [`LOCAL_PATCHES.md`](LOCAL_PATCHES.md) and re-verify each
patch.

## Conventions

- Record the upstream commit range absorbed (`<old>..<new>`).
- Note any conflicts hit and how they were resolved.
- Note any local patches superseded by the merge.
- Recommended remote setup (set once per local clone):
  ```
  git remote add upstream https://github.com/nextlevelbuilder/goclaw.git
  git fetch upstream
  ```

## Sync procedure

```
git fetch upstream
git checkout dev
git merge upstream/main          # or upstream/dev, depending on what's being absorbed
# resolve conflicts, re-verify LOCAL_PATCHES.md entries
git push origin dev
```

---

## Log

### 2026-05-11 — baseline

- **Upstream commit at fork point:** `c651cde5` (Merge branch 'dev' into main)
- **Fork primary branch:** `dev`
- **Notes:** First entry. No merge performed — this records the upstream commit
  the fork currently sits at, so future syncs have a known base.
