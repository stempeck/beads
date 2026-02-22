IMPLREADME.md. IMPORTANT! DO NOT modify the IMPLREADME.md! It should be used as the source-of-truth for implementation details and can only be modified if I explicitely ask.

## Requirements

When the /beads/ project is newly installed in a docker container and utilized by our 3rd party application gastown, usage goes something like:
Details in: `./IMPLBEADS.md`

---

### scenario

Problem: 
  Gap 1: No formal bond in the cross-prefix case

  In the same-prefix path, bd mol bond creates a bidirectional molecule linking wisp and target bead. In the cross-prefix path, that link doesn't exist — the   
  relationship is implicit through formula variables. If downstream code (refinery, completion tracking) needs to traverse from wisp back to original bead, it  
  has to parse variables instead of following bonds.

  Gap 2: Asymmetric error handling

  Same-prefix bond failure is fatal (return fmt.Errorf). Cross-prefix bd update failure is non-fatal (warning log, continues). The reviewer noted this asymmetry
   is intentional but worth flagging.

  Gap 3: No assignee on the original bead

  In the cross-prefix path, the original bead gets --status=hooked but no --assignee. The wisp root gets both. Dashboard consumers can't see who's working on   
  the original bead without tracing through to the wisp.

  To close the gaps the reviewer identified, you'd need changes outside the scope of this fix:

  1. Upstream bd CLI change: Make bd mol bond support cross-prefix routing (like bd show already does). This eliminates the workaround entirely — wisp and bead 
  bond normally regardless of prefix. That's the real fix, but it's in a different codebase.
  2. Explicit cross-prefix linking: Create a new metadata mechanism (e.g., bd update --linked-to=br-lsx on the wisp) so the relationship is queryable without   
  parsing formula variables. This requires a bd feature addition.
  3. Symmetric error handling: Make the cross-prefix bd update failure fatal too, or make the same-prefix bond failure non-fatal. Either way, align them. This  
  is a design decision about how hard to fail.
  4. Assignee propagation: Add --assignee to the cross-prefix bd update call on the original bead. This is the simplest fix — maybe a 2-line change — but the   
  code review noted it's "likely intentional" since the wisp root is the actual work unit.

A hack/fix was put in place that correctly solves the immediate gastown beads use problem to make: `gt sling mol-terraform-fix --on br-lsx <rig>` work, but it's a workaround for a limitation in bd mol bond, not a first-principles architectural fix. A 10/10 would require fixing bd itself so bond supports cross-prefix routing — making the entire workaround unnecessary.

---

Assume *.md documents might have outdated information and you should review the codebase as the only real source-of-truth and only use them to help in your search. You should also rely on ./docs/*.md files for understanding the archtictural intentions of beads before making recommendations.

### Acceptance criteria
The ONLY successfull outcome of this task is that we're able to follow the process steps described to setup a fresh rig with clean repo and when gastown uses beads, we should be able to operate without doctor fixes or human interaction (unless absolutely necessary, or in the case of doctor - only as a bandaid/fix for legacy broken behaviors). The beads should work as expected in the gastown environment so the AI workers utilizing beads can perform their work so that we have consistent successful outcomes out of each worker utilizng beads. Your mission is to seek to understand how to achieve this with beads. Perform scientific best practices in root cause analysis and test various scenarios to fully understand the root cause of any blockers to progressing and create a analysis.md with details necessary to implementing our outcome.
