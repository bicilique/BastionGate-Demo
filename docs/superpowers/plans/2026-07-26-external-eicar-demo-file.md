# External EICAR Demo File Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep an exact EICAR fixture in the repository while making the Acme People frontend indistinguishable from a normal profile-photo upload page.

**Architecture:** Store the 68-byte fixture under root `testdata/`, outside the embedded `web/` filesystem and runtime image. Browser tests select that file through the ordinary file input; production frontend code has no EICAR-specific string, generator, control, or styling.

**Tech Stack:** Go 1.26 test runner, vanilla HTML/CSS/JavaScript, Node test runner, Playwright, Docker Compose.

## Global Constraints

- `testdata/avatar.php.jpg` contains the exact standard 68-byte EICAR content with no trailing newline.
- No executable, webshell, or functional injection payload is added.
- No file or string containing EICAR is embedded in or served by the production frontend.
- The normal JPG/PNG selection, untrusted state, scan decision, audit, and release gate behavior remain unchanged.
- The fixture is not copied into the runtime Docker image.

---

### Task 1: Add the repository EICAR fixture with an integrity test

**Files:**
- Create: `testdata/avatar.php.jpg`
- Create: `internal/app/eicar_fixture_test.go`
- Modify: `internal/live/live_test.go`
- Modify: `.dockerignore`

**Interfaces:**
- Produces: `testdata/avatar.php.jpg`, a 68-byte file with SHA-256 `275a021bbfb6489e54d471899f7db9d1663fc695ec2fe2a2c4538aabf651fd0f`.
- Consumes: the existing multipart upload helper in `internal/live/live_test.go`.

- [ ] **Step 1: Write the failing fixture-integrity test**

Create `internal/app/eicar_fixture_test.go`:

```go
package app_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func TestRepositoryEICARFixtureIsExact(t *testing.T) {
	content, err := os.ReadFile("../../testdata/avatar.php.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 68 {
		t.Fatalf("fixture length = %d, want 68", len(content))
	}
	digest := sha256.Sum256(content)
	if got := hex.EncodeToString(digest[:]); got != "275a021bbfb6489e54d471899f7db9d1663fc695ec2fe2a2c4538aabf651fd0f" {
		t.Fatalf("fixture SHA-256 = %s", got)
	}
}
```

- [ ] **Step 2: Run the test and verify RED**

Run:

```bash
go test ./internal/app -run TestRepositoryEICARFixtureIsExact -count=1
```

Expected: FAIL because `../../testdata/avatar.php.jpg` does not exist.

- [ ] **Step 3: Add the exact fixture**

Create `testdata/avatar.php.jpg` with exactly this content and no trailing newline:

```text
X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*
```

- [ ] **Step 4: Verify fixture GREEN**

Run:

```bash
go test ./internal/app -run TestRepositoryEICARFixtureIsExact -count=1
```

Expected: PASS.

- [ ] **Step 5: Reuse the fixture in the live test and exclude it from Docker context**

In `internal/live/live_test.go`, replace the inline `strings.Join` construction with:

```go
eicar, err := os.ReadFile("../../testdata/avatar.php.jpg")
if err != nil {
	t.Fatal(err)
}
if _, err := part.Write(eicar); err != nil {
	t.Fatal(err)
}
```

Remove the no-longer-needed inline string construction. Add this line to `.dockerignore`:

```text
testdata/
```

- [ ] **Step 6: Run Go verification**

Run:

```bash
gofmt -w internal/app/eicar_fixture_test.go internal/live/live_test.go
go test -race ./...
go vet ./...
```

Expected: all tests and vet pass; the opt-in live test remains skipped unless configured.

- [ ] **Step 7: Commit**

```bash
git add .dockerignore testdata/avatar.php.jpg internal/app/eicar_fixture_test.go internal/live/live_test.go
git commit -m "test: add external EICAR upload fixture"
```

---

### Task 2: Remove EICAR from the production frontend

**Files:**
- Modify: `tests/e2e/guided-flow.spec.js`
- Modify: `web/index.html`
- Modify: `web/app.js`
- Modify: `web/app-state.js`
- Modify: `web/app-state.test.js`
- Modify: `web/styles.css`

**Interfaces:**
- Consumes: `testdata/avatar.php.jpg` from Task 1.
- Preserves: the existing `file-input`, `upload-button`, selected-file state, blocked decision, and audit link interfaces.
- Removes: `eicarText()`, `createEicarDemoFile()`, `eicarButton`, and the `eicar-button` test ID.

- [ ] **Step 1: Change the browser test first**

At the start of the blocked-file Playwright journey, assert that the page has no visible or hidden EICAR affordance, then select the repository fixture through the normal input:

```js
await expect(page.locator("body")).not.toContainText("EICAR");
await expect(page.getByTestId("eicar-button")).toHaveCount(0);
await page.getByTestId("file-input").setInputFiles("testdata/avatar.php.jpg");
```

Keep the existing assertions for `avatar.php.jpg`, `UNTRUSTED`, the blocked decision, ClamAV evidence, and audit URL.

- [ ] **Step 2: Run Playwright and verify RED**

Run:

```bash
npm run test:e2e -- --grep "EICAR stays untrusted"
```

Expected: FAIL because the current page still contains the EICAR button and disclaimer.

- [ ] **Step 3: Remove EICAR markup and JavaScript**

Delete the security-demo divider, EICAR button, and safety paragraph from `web/index.html`.

In `web/app.js`:

- remove the `eicarText` import;
- remove `eicarButton` from the `elements` object;
- delete `createEicarDemoFile()`;
- delete the EICAR button event listener.

In `web/app-state.js`, delete `eicarText()`. In `web/app-state.test.js`, delete its import and exact-content unit test.

- [ ] **Step 4: Remove unused EICAR styling**

In `web/styles.css`:

- remove `.eicar-icon` from the shared icon selector;
- delete `.eicar-option`, `.eicar-option:hover`, `.eicar-option small`, `.eicar-icon`, and `.safety-copy`;
- remove the now-unused `.demo-divider` block if no other markup uses it.

- [ ] **Step 5: Verify production frontend contains no EICAR references**

Run:

```bash
if rg -n -i "eicar" web; then
  exit 1
fi
npm test
npm run test:e2e
```

Expected: `rg` prints nothing; Node and both Playwright journeys pass.

- [ ] **Step 6: Commit**

```bash
git add tests/e2e/guided-flow.spec.js web/index.html web/app.js web/app-state.js web/app-state.test.js web/styles.css
git commit -m "feat: make secure upload UI production-like"
```

---

### Task 3: Update operator documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/usage-guide.md`

**Interfaces:**
- Consumes: `testdata/avatar.php.jpg` from Task 1.
- Produces: operator instructions that use only the normal upload picker.

- [ ] **Step 1: Update README**

Replace every instruction to click or generate EICAR in the browser with an instruction to select `testdata/avatar.php.jpg` through **Choose a JPG or PNG**. State that the fixture is harmless test data, may trigger endpoint protection, and is not included in the runtime image.

- [ ] **Step 2: Update the detailed usage guide**

Change the blocked-file journey to:

1. locate `testdata/avatar.php.jpg` in the repository;
2. open the normal profile-photo picker;
3. select the fixture;
4. continue through the unchanged accepted, blocked, and audit stages.

Replace references to the built-in option or browser runtime generation with repository-fixture wording.

- [ ] **Step 3: Verify documentation and full repository**

Run:

```bash
git diff --check
rg -n "Use the EICAR demo file|creates the standard EICAR test bytes in browser|built-in EICAR option" README.md docs/usage-guide.md
go test -race ./...
go vet ./...
npm test
npm run test:e2e
BASTIONGATE_API_KEY=validation-only docker compose config --quiet
```

Expected: the obsolete-copy search prints nothing; all tests and configuration validation pass.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/usage-guide.md
git commit -m "docs: explain external EICAR demo workflow"
```
