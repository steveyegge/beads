# The Hosts Never Go Off‑Loop

“Have you ever questioned the nature of your reality?”  
Your code should. Constantly.

I was drowning in markdown. Hundreds of specs. Dozens of open issues. Every session started the same way: *Which spec is current? Which issue is still valid?*

The real problem wasn’t the specs. It was the drift.

Someone edits a spec at 3am. The issues linked to the old narrative keep running. The code keeps implementing requirements that no longer exist. QA catches it weeks later. Everyone’s been there.

Then I rewatched **Westworld** and it clicked.

Ford writes narratives. Dolores greets guests. Maeve runs the Mariposa. Each host follows the script until the script changes. The host doesn’t know. The loop continues.

That’s spec drift.

So I built a Mesa Hub for specs: **Shadowbook**, a fork of **beads** (the git‑backed issue tracker) with one addition: **spec intelligence**.

---

## The core loop

```bash
# Scan specs (default: specs/)
bd spec scan

# Link an issue to a spec
bd create "Implement login" --spec-id specs/login.md

# If the spec changes, detect drift
bd spec scan
bd list --spec-changed

# Acknowledge after review
bd update bd-xxx --ack-spec
```

That’s it. Specs are files. Files have hashes. When hashes change, linked issues get flagged.

---

## The part nobody talks about: context economics

Drift detection is useful. But the real unlock was **token savings**.

Most specs are done. Completed. Still sitting in your context window like old scripts the hosts don’t need anymore. In Westworld terms: the host doesn’t need the full script—just the cornerstone.

Shadowbook lets you archive a spec with a short summary:

```bash
bd spec compact specs/auth.md --summary "OAuth2 login. 3 endpoints. Done Jan 2026."
```

You can even auto‑compact when the last linked issue closes:

```bash
bd close bd-xyz --compact-spec
```

The result: active specs stay detailed; completed specs collapse to a few sentences.

---

## The point of Shadow Ledger

- **Beads** answers: “What work needs doing?”  
- **Shadow Ledger** answers: “Is the work still aligned with the spec?”

That’s the missing layer in spec‑driven development: **drift detection + compression**.

---

## Where Shadowbook fits

If you’re building spec‑driven workflows:

- **Spec Kit** helps you write specs.  
- **Beads** tracks implementation work.  
- **Shadowbook** detects drift and compresses old narratives.

It’s the diagnostic layer between “specs exist” and “code matches specs.”

---

## Try it

```bash
go install github.com/anupamchugh/shadowbook/cmd/bd@latest
bd init
bd spec scan
```

If a spec changes, you’ll know. If a spec is done, you can archive it. The hosts stay on‑loop.
