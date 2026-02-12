IMPLREADME.md. IMPORTANT! DO NOT modify the IMPLREADME.md! It should be used as the source-of-truth for implementation details and can only be modified if I explicitely ask.

## Requirements

When the /beads/ project is newly installed in a docker container and utilized by our 3rd party application gastown, usage goes something like:
Details in: `./IMPLBEADS.md`

---

### scenario

Problem: 

---

Assume *.md documents might have outdated information and you should review the codebase as the only real source-of-truth and only use them to help in your search. You should also rely on ./docs/*.md files for understanding the archtictural intentions of beads before making recommendations.

### Acceptance criteria
The ONLY successfull outcome of this task is that we're able to follow the process steps described to setup a fresh rig with clean repo and when gastown uses beads, we should be able to operate without doctor fixes or human interaction (unless absolutely necessary, or in the case of doctor - only as a bandaid/fix for legacy broken behaviors). The beads should work as expected in the gastown environment so the AI workers utilizing beads can perform their work so that we have consistent successful outcomes out of each worker utilizng beads. Your mission is to seek to understand how to achieve this with beads. Perform scientific best practices in root cause analysis and test various scenarios to fully understand the root cause of any blockers to progressing and create a analysis.md with details necessary to implementing our outcome.
