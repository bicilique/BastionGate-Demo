# External EICAR Demo File Design

## Goal

Make Acme People look and behave like a normal profile-photo application. The upload screen must not mention EICAR, security test files, malicious samples, or demo-only file generation.

The repository retains a harmless EICAR test file so a demo operator can select it through the same JPG/PNG file picker used for ordinary profile photos.

## User experience

The first stage contains only:

- the existing profile context;
- a JPG/PNG file picker and drag-and-drop area;
- the selected-file preview and `UNTRUSTED` label;
- the normal **Send through BastionGate** action.

There is no EICAR button, disclaimer, divider, icon, hidden control, query parameter, or browser-side generator. The production frontend bundle contains no EICAR string.

After selection, both the repository EICAR file and a normal image follow the same visible flow: accepted submission, scanning, trust decision, and audit evidence.

## Repository test file

The repository stores `testdata/avatar.php.jpg` with the exact 68-byte standard EICAR content and no trailing newline. The disguised double-extension filename preserves the upload-security demonstration while remaining selectable as an image by filename.

The file is test data, not an application asset:

- it is not embedded by the Go web asset package;
- it is not copied into the runtime Docker image;
- it is never served by an HTTP route;
- documentation identifies it as harmless antivirus test data;
- no executable, webshell, or functional injection payload is included.

## Tests

The browser blocked-file journey reads `testdata/avatar.php.jpg` from the test runner and uploads it through the ordinary file input. This verifies the real user-visible path without adding test affordances to the UI.

The frontend unit test for the browser EICAR generator is removed because the generator no longer exists. A repository-level Go test verifies the committed file is exactly 68 bytes and has the standard SHA-256 digest.

The clean-image Playwright journey, release gate tests, backend streaming tests, and opt-in live BastionGate test remain.

## Documentation

README and the usage guide instruct demo operators to choose `testdata/avatar.php.jpg` through the normal upload field. They explicitly warn that endpoint-security software may alert on or quarantine the standard EICAR test file.

The original design and implementation plan are historical records. This design supersedes only their browser-runtime EICAR generation requirement; all quarantine-first, decision, audit, and release-control requirements remain unchanged.
