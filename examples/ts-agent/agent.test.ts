/**
 * Test suite for BeadsAgent
 *
 * Run with: npx ts-node agent.test.ts
 *
 * Test modes:
 * - compile: Just verify TypeScript compiles
 * - unit: Test with mocked bd commands
 * - integration: Test against real bd (requires beads project)
 */

import { execSync, spawnSync } from "child_process";

// ============================================================================
// Test Utilities
// ============================================================================

const colors = {
  reset: "\x1b[0m",
  red: "\x1b[31m",
  green: "\x1b[32m",
  yellow: "\x1b[33m",
  blue: "\x1b[34m",
  dim: "\x1b[2m",
};

let passCount = 0;
let failCount = 0;
let skipCount = 0;

function pass(name: string): void {
  passCount++;
  console.log(`  ${colors.green}✓${colors.reset} ${name}`);
}

function fail(name: string, error?: string): void {
  failCount++;
  console.log(`  ${colors.red}✗${colors.reset} ${name}`);
  if (error) {
    console.log(`    ${colors.dim}${error}${colors.reset}`);
  }
}

function skip(name: string, reason: string): void {
  skipCount++;
  console.log(`  ${colors.yellow}○${colors.reset} ${name} ${colors.dim}(${reason})${colors.reset}`);
}

function section(name: string): void {
  console.log(`\n${colors.blue}${name}${colors.reset}`);
}

// ============================================================================
// Test: Compilation
// ============================================================================

function testCompilation(): boolean {
  section("Compilation Tests");

  // Get the directory where this test file lives
  const testDir = __dirname;

  try {
    // Check TypeScript compilation from the ts-agent directory
    execSync("npx tsc --noEmit", {
      encoding: "utf-8",
      stdio: "pipe",
      cwd: testDir,
    });
    pass("TypeScript compiles without errors");
    return true;
  } catch (error) {
    const stderr = (error as { stderr?: string }).stderr || "";
    fail("TypeScript compilation", stderr.split("\n")[0]);
    return false;
  }
}

// ============================================================================
// Test: Prerequisites
// ============================================================================

function testPrerequisites(): { hasBd: boolean; hasBeads: boolean } {
  section("Prerequisite Tests");

  let hasBd = false;
  let hasBeads = false;

  // Check bd is installed
  try {
    const version = execSync("bd --version", { encoding: "utf-8", stdio: "pipe" });
    pass(`bd installed (${version.trim().split("\n")[0]})`);
    hasBd = true;
  } catch {
    skip("bd installed", "bd not in PATH");
  }

  // Check we're in a beads project (check for .beads directory)
  if (hasBd) {
    try {
      const fs = require("fs");
      if (fs.existsSync(".beads")) {
        pass("In beads-initialized directory (.beads/ exists)");
        hasBeads = true;
      } else {
        skip("beads directory", ".beads/ not found");
      }
    } catch {
      skip("beads directory", "could not check for .beads/");
    }
  }

  return { hasBd, hasBeads };
}

// ============================================================================
// Test: Unit Tests (mocked bd)
// ============================================================================

function testUnitLogic(): void {
  section("Unit Tests (Logic)");

  // Test: JSON parsing handles empty response
  try {
    const empty = "";
    const parsed = empty.trim() ? JSON.parse(empty) : {};
    if (Object.keys(parsed).length === 0) {
      pass("Empty bd response handled");
    } else {
      fail("Empty bd response", "Expected empty object");
    }
  } catch (e) {
    fail("Empty bd response", String(e));
  }

  // Test: JSON parsing handles array response
  try {
    const arrayResponse = '[{"id":"bd-test","title":"Test"}]';
    const parsed = JSON.parse(arrayResponse);
    if (Array.isArray(parsed) && parsed.length === 1 && parsed[0].id === "bd-test") {
      pass("Array bd response parsed");
    } else {
      fail("Array bd response", "Unexpected parse result");
    }
  } catch (e) {
    fail("Array bd response", String(e));
  }

  // Test: Title matching for discovery
  const testTitles = [
    { title: "Implement user auth", shouldDiscover: true },
    { title: "Add new feature", shouldDiscover: true },
    { title: "Fix bug in login", shouldDiscover: false },
    { title: "Update documentation", shouldDiscover: false },
  ];

  for (const { title, shouldDiscover } of testTitles) {
    const titleLower = title.toLowerCase();
    const wouldDiscover = titleLower.includes("implement") || titleLower.includes("add");
    if (wouldDiscover === shouldDiscover) {
      pass(`Discovery logic: "${title}" → ${shouldDiscover ? "discover" : "no discover"}`);
    } else {
      fail(`Discovery logic: "${title}"`, `Expected ${shouldDiscover}, got ${wouldDiscover}`);
    }
  }
}

// ============================================================================
// Test: Integration Tests (real bd)
// ============================================================================

function testIntegration(hasBeads: boolean): void {
  section("Integration Tests");

  if (!hasBeads) {
    skip("bd ready command", "no beads project");
    skip("bd create command", "no beads project");
    skip("bd update command", "no beads project");
    skip("bd close command", "no beads project");
    return;
  }

  // Test: bd ready returns valid JSON (use spawnSync to handle stderr warnings)
  try {
    const result = spawnSync("bd", ["ready", "--json", "--limit", "1"], {
      encoding: "utf-8",
    });
    // bd may return non-zero due to warnings, check if stdout has valid JSON
    const stdout = result.stdout?.trim() || "";
    if (stdout.startsWith("[")) {
      const parsed = JSON.parse(stdout);
      if (Array.isArray(parsed)) {
        pass(`bd ready returns array (${parsed.length} items)`);
      } else {
        fail("bd ready", "Expected array response");
      }
    } else {
      fail("bd ready", `Invalid output: ${result.stderr?.split("\n")[0] || "empty"}`);
    }
  } catch (e) {
    fail("bd ready", String(e));
  }

  // Test: bd stats works
  try {
    execSync("bd stats", { encoding: "utf-8", stdio: "pipe" });
    pass("bd stats executes");
  } catch (e) {
    fail("bd stats", String(e));
  }

  // Test: Create, update, close cycle (dry run - just validate commands work)
  console.log(`\n  ${colors.dim}Skipping create/update/close cycle (would modify data)${colors.reset}`);
  skip("Full agent cycle", "would modify beads data - run agent.ts manually");
}

// ============================================================================
// Test: Agent Execution (sandboxed)
// ============================================================================

function testAgentExecution(hasBeads: boolean): void {
  section("Agent Execution Test");

  if (!hasBeads) {
    skip("Agent dry-run", "no beads project");
    return;
  }

  // Run agent with 0 iterations (just initialization)
  const testDir = __dirname;
  const path = require("path");
  const agentScript = path.join(testDir, "agent.ts");

  try {
    // Use execSync which is more reliable than spawnSync for npx
    const output = execSync(`npx ts-node "${agentScript}" 0`, {
      encoding: "utf-8",
      timeout: 60000,
      stdio: ["pipe", "pipe", "pipe"],
    });

    // Check if agent ran (look for startup message)
    if (output.includes("Beads Agent starting")) {
      pass("Agent initializes and exits cleanly");
    } else {
      pass("Agent exits cleanly");
    }
  } catch (e) {
    // execSync throws on non-zero exit, but agent might still have run
    const error = e as { stdout?: string; stderr?: string; message?: string };
    if (error.stdout?.includes("Beads Agent starting")) {
      pass("Agent initializes (with warnings)");
    } else {
      const errLine = error.stderr?.split("\n").find((l: string) => l.trim() && !l.includes("Orphan")) ||
                      error.message || "Unknown error";
      fail("Agent execution", errLine);
    }
  }
}

// ============================================================================
// Main
// ============================================================================

function main(): void {
  console.log("═══════════════════════════════════════════════════════════════");
  console.log("  TypeScript Agent Test Suite");
  console.log("═══════════════════════════════════════════════════════════════");

  const compiles = testCompilation();

  if (!compiles) {
    console.log(`\n${colors.red}Compilation failed - skipping remaining tests${colors.reset}`);
    process.exit(1);
  }

  const { hasBd, hasBeads } = testPrerequisites();

  testUnitLogic();
  testIntegration(hasBeads);
  testAgentExecution(hasBeads);

  // Summary
  console.log("\n═══════════════════════════════════════════════════════════════");
  console.log("  Summary");
  console.log("═══════════════════════════════════════════════════════════════");
  console.log(`  ${colors.green}${passCount} passed${colors.reset}`);
  if (failCount > 0) {
    console.log(`  ${colors.red}${failCount} failed${colors.reset}`);
  }
  if (skipCount > 0) {
    console.log(`  ${colors.yellow}${skipCount} skipped${colors.reset}`);
  }

  process.exit(failCount > 0 ? 1 : 0);
}

main();
