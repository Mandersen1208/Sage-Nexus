# SOUL Runtime

Sage Nexus keeps the current real SOUL.md file as the default source:

```text
/home/node/.openclaw/workspace/SOUL.md
```

In Docker, this path is provided by mounting the host workspace directory at `/home/node/.openclaw/workspace`.

Override with:

```text
SAGE_SOUL_PATH=/path/to/SOUL.md
```

If SOUL is missing or unreadable, the manager falls back to the bundled Sage prompt in `services/manager/prompts/sage.md`.

The intended future path is Sage-owned state:

```text
/sage-state/workspace/SOUL.md
```

Do not load SOUL from multiple runtime config surfaces. `SAGE_SOUL_PATH` should be the source of truth.
