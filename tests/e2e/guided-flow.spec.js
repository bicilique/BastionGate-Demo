import { expect, test } from "@playwright/test";

test("EICAR stays untrusted, is blocked, and links to the real audit route", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByText("BastionGate connected")).toBeVisible();

  await page.getByTestId("eicar-button").click();
  await expect(page.locator("#selectedFileName")).toHaveText("avatar.php.jpg");
  await expect(page.getByText("UNTRUSTED", { exact: true })).toBeVisible();

  await page.getByTestId("upload-button").click();
  await expect(page.getByRole("heading", { name: "Your photo was blocked" })).toBeVisible();
  await expect(page.getByText("ClamAV detected Eicar-Signature")).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry technical report" })).toBeHidden();

  await page.getByRole("button", { name: "Continue to audit" }).click();
  await expect(page.getByTestId("audit-link")).toHaveAttribute(
    "href",
    "http://localhost:8080/files/123e4567-e89b-12d3-a456-426614174001",
  );
  await page.getByRole("button", { name: "View technical report" }).click();
  await page.getByRole("button", { name: "Redacted JSON" }).click();
  await expect(page.locator("#rawReport")).toContainText("Eicar-Signature");
  await expect(page.locator("#rawReport")).not.toContainText("private-bucket");
});

test("a clean file becomes the avatar only after BastionGate releases it", async ({ page }) => {
  await page.goto("/");
  await page.getByTestId("file-input").setInputFiles({
    name: "clean.png",
    mimeType: "image/png",
    buffer: Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
      "base64",
    ),
  });

  await expect(page.locator("#releasedAvatar")).toBeHidden();
  await page.getByTestId("upload-button").click();
  await expect(page.getByRole("heading", { name: "Your photo is ready" })).toBeVisible();
  await expect(page.locator("#releasedAvatar")).toHaveAttribute("src", /^blob:/);
  await page.getByRole("button", { name: "Continue to audit" }).click();
  await page.getByRole("button", { name: "Run another demo" }).click();
  await expect(page.locator("#releasedAvatar")).toBeVisible();
});
