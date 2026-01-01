#!/usr/bin/env npx ts-node
/**
 * Simple AI agent workflow using bd (Beads issue tracker).
 *
 * This demonstrates how an agent can:
 * 1. Find ready work
 * 2. Claim and execute tasks
 * 3. Discover new issues during work
 * 4. Link discoveries back to parent tasks
 * 5. Complete work and move on
 */

import { execSync, spawnSync } from "child_process";

// Types
interface BeadsIssue {
  id: string;
  title: string;
  description?: string;
  status: string;
  priority: number;
  issue_type: string;
  created_at: string;
  updated_at: string;
}

// Logging utilities with colors
const colors = {
  reset: "\x1b[0m",
  red: "\x1b[31m",
  green: "\x1b[32m",
  yellow: "\x1b[33m",
  blue: "\x1b[34m",
};

function logInfo(msg: string): void {
  console.log(`${colors.blue}ℹ ${colors.reset}${msg}`);
}

function logSuccess(msg: string): void {
  console.log(`${colors.green}✓${colors.reset} ${msg}`);
}

function logWarning(msg: string): void {
  console.log(`${colors.yellow}⚠${colors.reset} ${msg}`);
}

function logError(msg: string): void {
  console.log(`${colors.red}✗${colors.reset} ${msg}`);
}

/**
 * BeadsAgent - Simple agent that manages tasks using bd CLI.
 */
class BeadsAgent {
  private currentTask: BeadsIssue | null = null;

  /**
   * Run a bd command and parse JSON output.
   */
  private runBd(...args: string[]): unknown {
    const cmd = ["bd", ...args, "--json"].join(" ");
    try {
      const result = execSync(cmd, {
        encoding: "utf-8",
        stdio: ["pipe", "pipe", "pipe"],
      });
      if (result.trim()) {
        return JSON.parse(result);
      }
      return {};
    } catch (error) {
      if (error instanceof Error && "stderr" in error) {
        throw new Error(`bd command failed: ${(error as { stderr: string }).stderr}`);
      }
      throw error;
    }
  }

  /**
   * Run a bd command without JSON output (for commands like dep add).
   */
  private runBdRaw(...args: string[]): void {
    const result = spawnSync("bd", args, { encoding: "utf-8" });
    if (result.status !== 0) {
      throw new Error(`bd command failed: ${result.stderr}`);
    }
  }

  /**
   * Find the highest priority ready work.
   */
  findReadyWork(): BeadsIssue | null {
    const ready = this.runBd("ready", "--limit", "1") as BeadsIssue[];

    if (Array.isArray(ready) && ready.length > 0) {
      return ready[0];
    }
    return null;
  }

  /**
   * Claim a task by setting status to in_progress.
   */
  claimTask(issueId: string): void {
    console.log(`📋 Claiming task: ${issueId}`);
    this.runBd("update", issueId, "--status", "in_progress");
    logSuccess("Task claimed");
  }

  /**
   * Create a new issue.
   */
  createIssue(
    title: string,
    options: {
      description?: string;
      priority?: number;
      issueType?: string;
    } = {}
  ): BeadsIssue {
    console.log(`✨ Creating issue: ${title}`);

    const args = [
      "create",
      title,
      "-p",
      String(options.priority ?? 2),
      "-t",
      options.issueType ?? "task",
    ];

    if (options.description) {
      args.push("-d", options.description);
    }

    return this.runBd(...args) as BeadsIssue;
  }

  /**
   * Link a discovered issue back to its parent.
   */
  linkDiscovery(discoveredId: string, parentId: string): void {
    console.log(`🔗 Linking ${discoveredId} ← discovered-from ← ${parentId}`);
    this.runBdRaw("dep", "add", discoveredId, parentId, "--type", "discovered-from");
    logSuccess("Dependency linked");
  }

  /**
   * Mark task as complete.
   */
  completeTask(issueId: string, reason: string = "Completed"): void {
    console.log(`✅ Completing task: ${issueId} - ${reason}`);
    this.runBd("close", issueId, "--reason", reason);
    logSuccess(`Task completed: ${issueId}`);
  }

  /**
   * Simulate doing work on an issue.
   *
   * In a real agent, this would call an LLM, execute code, etc.
   * Returns true if work discovered new issues.
   */
  simulateWork(issue: BeadsIssue): boolean {
    const { id: issueId, title, priority, issue_type } = issue;

    console.log(`\n🤖 Working on: ${title} (${issueId})`);
    console.log(`   Priority: ${priority}, Type: ${issue_type}`);

    // Simulate discovering a bug while working
    const titleLower = title.toLowerCase();
    if (titleLower.includes("implement") || titleLower.includes("add")) {
      console.log("\n💡 Discovered: Missing test coverage for this feature");

      const newIssue = this.createIssue(`Add tests for ${title}`, {
        description: `While implementing ${issueId}, noticed missing tests`,
        priority: 1,
        issueType: "task",
      });

      this.linkDiscovery(newIssue.id, issueId);
      return true;
    }

    return false;
  }

  /**
   * Execute one work cycle. Returns true if work was found.
   */
  runOnce(): boolean {
    // Find ready work
    const issue = this.findReadyWork();

    if (!issue) {
      console.log("📭 No ready work found.");
      return false;
    }

    // Claim the task
    this.claimTask(issue.id);
    this.currentTask = issue;

    // Do the work (simulated)
    const discoveredNewWork = this.simulateWork(issue);

    // Complete the task
    this.completeTask(issue.id, "Implemented successfully");
    this.currentTask = null;

    if (discoveredNewWork) {
      console.log("\n🔄 New work discovered and linked. Running another cycle...");
    }

    return true;
  }

  /**
   * Show beads statistics.
   */
  showStats(): void {
    console.log("\n═══════════════════════════════════════════════════");
    console.log("  Beads Statistics");
    console.log("═══════════════════════════════════════════════════");
    try {
      const stats = execSync("bd stats", { encoding: "utf-8" });
      console.log(stats);
    } catch {
      logWarning("Could not fetch stats");
    }
  }

  /**
   * Run the agent for multiple iterations.
   */
  run(maxIterations: number = 10): void {
    console.log("🚀 Beads Agent starting...\n");
    this.showStats();

    for (let i = 0; i < maxIterations; i++) {
      console.log(`\n${"=".repeat(60)}`);
      console.log(`Iteration ${i + 1}/${maxIterations}`);
      console.log("=".repeat(60));

      if (!this.runOnce()) {
        break;
      }
    }

    console.log("\n✨ Agent finished!");
    this.showStats();
  }
}

/**
 * Check prerequisites before running.
 */
function checkPrerequisites(): boolean {
  // Check if bd is installed
  try {
    execSync("bd --version", { encoding: "utf-8", stdio: "pipe" });
  } catch {
    logError("bd is not installed");
    console.log("Install with: go install github.com/steveyegge/beads/cmd/bd@latest");
    return false;
  }

  // Check if we're in a beads-initialized directory
  try {
    execSync("bd list --limit 1", { encoding: "utf-8", stdio: "pipe" });
  } catch {
    logError("Not in a beads-initialized directory");
    console.log("Run: bd init");
    return false;
  }

  return true;
}

/**
 * Main entry point.
 */
function main(): void {
  // Handle Ctrl+C gracefully
  process.on("SIGINT", () => {
    console.log("\n\n👋 Agent interrupted by user");
    process.exit(0);
  });

  if (!checkPrerequisites()) {
    process.exit(1);
  }

  try {
    const agent = new BeadsAgent();
    const maxIterations = parseInt(process.argv[2] ?? "10", 10);
    agent.run(maxIterations);
  } catch (error) {
    logError(`Error: ${error instanceof Error ? error.message : String(error)}`);
    process.exit(1);
  }
}

main();
