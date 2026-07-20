import {
  expect,
  test,
  type Browser,
  type BrowserContext,
  type Locator,
  type Page,
} from "@playwright/test";

type UserRole = "admin" | "teacher" | "student";

const mode = process.env.E2E_MODE?.trim() || "local";
const baseURL =
  mode === "staging"
    ? new URL(requiredEnvironment("E2E_BASE_URL")).origin
    : "http://127.0.0.1:5173";
const accountEmails =
  mode === "staging"
    ? ({
        admin:
          process.env.E2E_ADMIN_EMAIL?.trim() || "admin.e2e@tutorhub.local",
        student: requiredEnvironment("E2E_STUDENT_EMAIL"),
        teacher: requiredEnvironment("E2E_TEACHER_EMAIL"),
      } as const)
    : ({
        admin: "admin.e2e@tutorhub.local",
        student: "student.e2e@tutorhub.local",
        teacher: "teacher.e2e@tutorhub.local",
      } as const);
const storageStateKeys = {
  admin: "E2E_ADMIN_STORAGE_STATE",
  teacher: "E2E_TEACHER_STORAGE_STATE",
  student: "E2E_STUDENT_STORAGE_STATE",
} as const;
const localDisplayNames = {
  admin: "E2E Administrator",
  teacher: "E2E Teacher",
  student: "E2E Student",
} as const;

function requiredEnvironment(name: string) {
  const value = process.env[name]?.trim();
  if (!value) {
    throw new Error(`${name} is required for this E2E mode.`);
  }
  return value;
}

async function newRoleContext(browser: Browser, role: UserRole) {
  const storageState =
    mode === "staging"
      ? requiredEnvironment(storageStateKeys[role])
      : undefined;
  return browser.newContext({
    baseURL,
    locale: "vi-VN",
    storageState,
    viewport: { width: 1366, height: 900 },
  });
}

async function signIn(page: Page, role: UserRole) {
  if (mode === "staging") {
    await page.goto("/app/home");
    await expect(page).not.toHaveURL(/\/sign-in(?:\?|$)/);
    return;
  }

  await page.goto("/sign-in");
  await page.getByRole("main").getByRole("button").click();
  await expect(
    page.getByRole("heading", {
      name: "TutorHub test identity provider",
    }),
  ).toBeVisible();
  await page
    .getByRole("button", {
      name: `Sign in as ${localDisplayNames[role]} (${accountEmails[role]})`,
    })
    .click();
  await expect(page).toHaveURL(/\/app\/home$/);
}

async function useEnglish(page: Page) {
  const language = page.locator(".language-select select");
  await expect(language).toBeVisible();
  await language.selectOption("en");
  await expect(language).toHaveValue("en");
}

async function chooseRadixOption(
  page: Page,
  accessibleName: string | RegExp,
  optionName: string,
) {
  await page.getByRole("combobox", { name: accessibleName }).click();
  await page.getByRole("option", { name: optionName, exact: true }).click();
}

async function createMemberInvitation(
  page: Page,
  email: string,
  role: "Guest" | "Instructor" | "Learner",
) {
  await page.getByRole("button", { name: "Invite member" }).click();
  const dialog = page.getByRole("dialog", {
    name: "Create member invitation",
  });
  await dialog.getByLabel("Invitee email").fill(email);
  await chooseRadixOption(page, "Workspace role", role);
  await dialog.getByRole("button", { name: "Create invitation" }).click();
  const link = dialog.getByLabel("One-time acceptance link");
  await expect(link).toBeVisible();
  const invitationURL = await link.inputValue();
  await dialog.getByRole("button", { name: "Close invitation dialog" }).click();
  return invitationURL;
}

async function openSecretFragmentURL(
  page: Page,
  rawURL: string,
  expectedPath: "/invite",
) {
  const target = new URL(rawURL);
  const configuredOrigin = new URL(baseURL).origin;
  expect(target.origin).toBe(configuredOrigin);
  expect(target.pathname).toBe(expectedPath);
  expect(target.search === "").toBe(true);
  expect(target.hash.length).toBeGreaterThan(1);

  // Load a different document first so replacing the URL with the invitation
  // fragment cannot be treated as a same-document hash navigation.
  await page.goto("/app/home");
  await page.evaluate(
    ({ fragment, pathname }) => {
      window.location.replace(`${pathname}${fragment}`);
    },
    {
      fragment: target.hash,
      pathname: target.pathname,
    },
  );
  await expect
    .poll(
      () =>
        page.evaluate(() => ({
          fragmentScrubbed: window.location.hash === "",
          pathname: window.location.pathname,
        })),
      {
        message: `expected navigation to settle at ${expectedPath} with the secret fragment scrubbed`,
      },
    )
    .toEqual({
      fragmentScrubbed: true,
      pathname: expectedPath,
    });
}

async function acceptWorkspaceInvitation(
  page: Page,
  invitationURL: string,
  workspaceName: string,
) {
  await openSecretFragmentURL(page, invitationURL, "/invite");
  await expect(
    page.getByRole("heading", {
      name: /^(?:Lời mời tham gia workspace|Workspace invitation)$/,
    }),
  ).toBeVisible();
  await expect(page.getByText(workspaceName, { exact: true })).toBeVisible();
  await page
    .getByRole("button", {
      name: /^(?:Chấp nhận lời mời|Accept invitation)$/,
    })
    .click();
  await expect(
    page.getByRole("heading", {
      name: /^(?:Đã tham gia workspace|Workspace joined)$/,
    }),
  ).toBeVisible();

  const switchAction = page.getByRole("button", {
    name: /^(?:Chuyển sang workspace này|Switch to this workspace)$/,
  });
  if (await switchAction.isVisible()) {
    await switchAction.click();
  } else {
    await page
      .getByRole("link", {
        name: /^(?:Tiếp tục vào TutorHub|Continue to TutorHub)$/,
      })
      .click();
  }
  await expect(page).toHaveURL(/\/app\/home$/);
  await useEnglish(page);
}

async function closeDialog(page: Page, accessibleName: string) {
  await page.getByRole("button", { name: accessibleName }).click();
}

async function submitWorkspaceCreate(page: Page, button: Locator) {
  const csrfResponse = page.waitForResponse((candidate) => {
    const request = candidate.request();
    return (
      request.method() === "GET" &&
      new URL(candidate.url()).pathname === "/api/v1/auth/csrf"
    );
  });
  const createResponse = page
    .waitForResponse((candidate) => {
      const request = candidate.request();
      return (
        request.method() === "POST" &&
        new URL(candidate.url()).pathname === "/api/v1/tenants"
      );
    })
    .then(
      (response) => ({ response, status: "fulfilled" as const }),
      (error: Error) => ({ error, status: "rejected" as const }),
    );

  await button.click();
  const csrf = await csrfResponse;
  expect(
    csrf.status(),
    "GET /api/v1/auth/csrf should return HTTP 200 before workspace creation",
  ).toBe(200);

  const result = await createResponse;
  if (result.status === "rejected") {
    const formError = page.locator(".workspace-form__error");
    const message = (await formError.isVisible())
      ? (await formError.textContent())?.trim()
      : undefined;
    throw new Error(
      `POST /api/v1/tenants was not observed after CSRF HTTP 200. ${message ?? "No form error was visible."}`,
      { cause: result.error },
    );
  }

  expect(
    result.response.status(),
    "POST /api/v1/tenants should return HTTP 201",
  ).toBe(201);
}

test("P2-08 connects admin, instructor, and learner workflows through the real UI", async ({
  browser,
}) => {
  test.skip(
    mode === "staging" &&
      process.env.E2E_ALLOW_STAGING_MUTATIONS?.trim() !== "true",
    "Set E2E_ALLOW_STAGING_MUTATIONS=true only for a disposable staging fixture.",
  );

  const runID = Date.now().toString(36);
  const workspaceName = `P2-08 Academy ${runID}`;
  const updatedWorkspaceName = `${workspaceName} Updated`;
  const workspaceSlug = `p2-08-${runID}`;
  const alternateWorkspaceName = `P2-08 Alternate ${runID}`;
  const classCode = `P208${runID}`.toUpperCase().slice(0, 32);
  const className = `P2-08 Class ${runID}`;
  const updatedClassName = `${className} Updated`;
  const revokedEmail = `revoked.${runID}@example.test`;

  const contexts: BrowserContext[] = [];
  try {
    const adminContext = await newRoleContext(browser, "admin");
    contexts.push(adminContext);
    const teacherContext = await newRoleContext(browser, "teacher");
    contexts.push(teacherContext);
    const studentContext = await newRoleContext(browser, "student");
    contexts.push(studentContext);
    const adminPage = await adminContext.newPage();
    const teacherPage = await teacherContext.newPage();
    const studentPage = await studentContext.newPage();

    await test.step("administrator creates, edits, and switches workspace boundaries", async () => {
      await signIn(adminPage, "admin");
      await useEnglish(adminPage);
      const onboarding = adminPage.getByRole("heading", {
        name: "Create your first workspace",
      });
      if (await onboarding.isVisible()) {
        await adminPage
          .getByLabel("Organization or learning group name")
          .fill(workspaceName);
        await adminPage.getByLabel("Workspace address").fill(workspaceSlug);
        await submitWorkspaceCreate(
          adminPage,
          adminPage.getByRole("button", { name: "Create workspace" }),
        );
        await expect(onboarding).toBeHidden();
        await expect(
          adminPage.getByRole("navigation", { name: "Primary navigation" }),
        ).toBeVisible();
        await expect(adminPage.locator(".workspace-select")).toContainText(
          workspaceName,
        );
      } else {
        await adminPage.goto("/app/workspace");
        await useEnglish(adminPage);
        await adminPage
          .getByRole("button", { name: "Create workspace", exact: true })
          .click();
        const primaryWorkspaceDialog = adminPage.getByRole("dialog", {
          name: "Create another workspace",
        });
        await primaryWorkspaceDialog
          .getByLabel("Organization or learning group name")
          .fill(workspaceName);
        await primaryWorkspaceDialog
          .getByLabel("Workspace address")
          .fill(workspaceSlug);
        await submitWorkspaceCreate(
          adminPage,
          primaryWorkspaceDialog.getByRole("button", {
            name: "Create workspace",
          }),
        );
        const primaryWorkspaceSelector = adminPage.getByRole("combobox", {
          name: "Active workspace",
        });
        await expect(primaryWorkspaceDialog).toBeHidden();
        await expect(primaryWorkspaceSelector).toBeVisible();
        await expect(
          primaryWorkspaceSelector.locator("option:checked"),
        ).toHaveText(workspaceName);
      }

      await adminPage.goto("/app/workspace");
      await useEnglish(adminPage);
      await adminPage
        .getByRole("region", { name: "Workspace settings" })
        .getByLabel("Organization or learning group name")
        .fill(updatedWorkspaceName);
      await adminPage.getByRole("button", { name: "Save settings" }).click();
      await expect(
        adminPage.getByRole("status").filter({ hasText: "Workspace updated." }),
      ).toBeVisible();

      await adminPage
        .getByRole("button", { name: "Create workspace", exact: true })
        .click();
      const createDialog = adminPage.getByRole("dialog", {
        name: "Create another workspace",
      });
      await createDialog
        .getByLabel("Organization or learning group name")
        .fill(alternateWorkspaceName);
      await createDialog
        .getByLabel("Workspace address")
        .fill(`p2-08-alt-${runID}`);
      await submitWorkspaceCreate(
        adminPage,
        createDialog.getByRole("button", { name: "Create workspace" }),
      );
      const workspaceSelector = adminPage.getByRole("combobox", {
        name: "Active workspace",
      });
      await expect(workspaceSelector).toBeVisible();
      await expect(workspaceSelector.locator("option:checked")).toHaveText(
        alternateWorkspaceName,
      );
      await workspaceSelector.selectOption({ label: updatedWorkspaceName });
      await expect(adminPage).toHaveURL(/\/app\/home$/);
      await expect(workspaceSelector.locator("option:checked")).toHaveText(
        updatedWorkspaceName,
      );
    });

    let teacherInvitation = "";
    let studentInvitation = "";
    await test.step("administrator invites two members and revokes a third invitation", async () => {
      await adminPage.goto("/app/workspace");
      await useEnglish(adminPage);
      teacherInvitation = await createMemberInvitation(
        adminPage,
        accountEmails.teacher,
        "Instructor",
      );
      studentInvitation = await createMemberInvitation(
        adminPage,
        accountEmails.student,
        "Learner",
      );
      await createMemberInvitation(adminPage, revokedEmail, "Guest");

      const revokedRow = adminPage
        .getByRole("listitem")
        .filter({ hasText: revokedEmail });
      await revokedRow
        .getByRole("button", {
          name: `Revoke invitation for ${revokedEmail}`,
        })
        .click();
      const revokeDialog = adminPage.getByRole("dialog", {
        name: "Revoke invitation?",
      });
      await revokeDialog
        .getByRole("button", { name: "Confirm revoke" })
        .click();
      await expect(
        revokedRow.getByText("Revoked", { exact: true }),
      ).toBeVisible();
    });

    await test.step("instructor and learner preview and accept their invitations", async () => {
      await signIn(teacherPage, "teacher");
      await acceptWorkspaceInvitation(
        teacherPage,
        teacherInvitation,
        updatedWorkspaceName,
      );
      await signIn(studentPage, "student");
      await acceptWorkspaceInvitation(
        studentPage,
        studentInvitation,
        updatedWorkspaceName,
      );
    });

    let classJoinURL = "";
    await test.step("instructor creates, edits, and activates a class and join link", async () => {
      await teacherPage.goto("/app/classrooms");
      await useEnglish(teacherPage);
      await teacherPage.getByRole("button", { name: "Create class" }).click();
      const createClassDialog = teacherPage.getByRole("dialog", {
        name: "Create a class",
      });
      await createClassDialog.getByLabel("Class code").fill(classCode);
      await createClassDialog.getByLabel("Class name").fill(className);
      await createClassDialog
        .getByLabel("Description")
        .fill("A complete P2-08 browser workflow.");
      await createClassDialog
        .getByRole("button", { name: "Create class" })
        .click();
      await expect(
        teacherPage.getByRole("heading", { name: className }),
      ).toBeVisible();

      const editRegion = teacherPage.getByRole("region", {
        name: "Edit class",
      });
      await editRegion.getByLabel("Class name").fill(updatedClassName);
      await chooseRadixOption(teacherPage, "Class status", "Active");
      await editRegion.getByRole("button", { name: "Save changes" }).click();
      await expect(
        editRegion
          .getByRole("status")
          .filter({ hasText: "The class was updated." }),
      ).toBeVisible();
      await expect(
        teacherPage.getByRole("heading", { name: updatedClassName }),
      ).toBeVisible();

      await teacherPage.getByRole("button", { name: "Create link" }).click();
      const linkDialog = teacherPage.getByRole("dialog", {
        name: "Create a class join link",
      });
      await linkDialog.getByRole("button", { name: "Create link" }).click();
      const linkField = linkDialog.getByLabel(
        "Class join link (shown only this time)",
      );
      await expect(linkField).toBeVisible();
      classJoinURL = await linkField.inputValue();
      await closeDialog(teacherPage, "Close class invitation dialog");
    });

    await test.step("learner joins by link and immediately sees the class list update", async () => {
      await studentPage.goto("/app/classrooms");
      await useEnglish(studentPage);
      await studentPage
        .getByRole("button", { name: "Join with a code" })
        .click();
      const joinDialog = studentPage.getByRole("dialog", {
        name: "Join a class",
      });
      await joinDialog.getByLabel("Join code or link").fill(classJoinURL);
      await joinDialog.getByRole("button", { name: "Join class" }).click();
      await expect(
        studentPage.getByRole("status").filter({ hasText: updatedClassName }),
      ).toBeVisible();
      await expect(
        studentPage.getByRole("link", { name: new RegExp(updatedClassName) }),
      ).toBeVisible();
    });

    await test.step("instructor updates the learner role, then suspends and removes the learner", async () => {
      await teacherPage.reload();
      await useEnglish(teacherPage);
      const studentRow = teacherPage
        .getByRole("row")
        .filter({ hasText: accountEmails.student });
      await expect(studentRow).toBeVisible();
      await studentRow.getByRole("combobox").click();
      await teacherPage
        .getByRole("option", { name: "Teaching assistant", exact: true })
        .click();
      await teacherPage
        .getByRole("dialog", { name: "Confirm roster change" })
        .getByRole("button", { name: "Confirm" })
        .click();
      await expect(
        teacherPage
          .getByRole("status")
          .filter({ hasText: "The class role was updated." }),
      ).toBeVisible();

      await studentRow.getByRole("button", { name: "Suspend" }).click();
      await teacherPage
        .getByRole("dialog", { name: "Confirm roster change" })
        .getByRole("button", { name: "Confirm" })
        .click();
      await expect(
        studentRow.getByText("Suspended", { exact: true }),
      ).toBeVisible();

      await studentRow
        .getByRole("button", { name: "Remove from class", exact: true })
        .click();
      await teacherPage
        .getByRole("dialog", { name: "Confirm roster change" })
        .getByRole("button", { name: "Confirm", exact: true })
        .click();
      await expect(
        studentRow.getByText("Removed", { exact: true }),
      ).toBeVisible();
    });

    await test.step("instructor revokes the link and archives the class", async () => {
      const inviteRegion = teacherPage.getByRole("region", {
        name: "Members and invite links",
      });
      await inviteRegion
        .getByRole("button", { name: /Revoke link expiring at/ })
        .click();
      await teacherPage
        .getByRole("dialog", { name: "Revoke this link?" })
        .getByRole("button", { name: "Confirm revoke" })
        .click();
      await expect(
        inviteRegion.getByText("Revoked", { exact: true }),
      ).toBeVisible();

      await teacherPage
        .getByRole("region", { name: "Archive class" })
        .getByRole("button", { name: "Archive class" })
        .click();
      await teacherPage
        .getByRole("dialog", { name: "Confirm class archive" })
        .getByRole("button", { name: "Confirm archive" })
        .click();
      await expect(
        teacherPage.getByText("Archived", { exact: true }).first(),
      ).toBeVisible();
    });

    await test.step("administrator reviews the resulting audit history responsively", async () => {
      await adminPage.goto("/app/workspace/audit");
      await useEnglish(adminPage);
      const auditTable = adminPage.getByRole("table", {
        name: "Workspace audit event list",
      });
      await expect(auditTable).toBeVisible();
      await expect(
        auditTable.getByRole("cell", { name: "Create class", exact: true }),
      ).toBeVisible();
      await expect(
        auditTable.getByRole("cell", {
          name: "Suspend class member",
          exact: true,
        }),
      ).toBeVisible();
      await expect(
        auditTable.getByRole("cell", {
          name: "Remove class member",
          exact: true,
        }),
      ).toBeVisible();

      await adminPage.setViewportSize({ width: 1024, height: 768 });
      await expect(
        adminPage.getByRole("heading", { name: "Activity audit log" }),
      ).toBeVisible();
      await adminPage.setViewportSize({ width: 390, height: 844 });
      await adminPage.getByRole("button", { name: "Open navigation" }).click();
      await expect(
        adminPage.getByRole("navigation", { name: "Primary navigation" }),
      ).toBeVisible();
    });
  } finally {
    await Promise.all(contexts.map((context) => context.close()));
  }
});
