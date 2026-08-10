// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { APIRequestError } from "@tutorhub/api-client";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { MediaLobbyPanel } from "./MediaLobbyPanel";
import { MediaSpaceInvitePanel } from "./MediaSpaceInvitePanel";

const lobbyMocks = vi.hoisted(() => ({
  admissions: vi.fn(),
  cancel: vi.fn(),
  invite: vi.fn(),
  members: vi.fn(),
  mutateMember: vi.fn(),
  resolve: vi.fn(),
}));

vi.mock("../app/mediaLobby", () => ({
  mediaLobbyIdempotencyKey: (action: string) => `media-${action}-fixed-key`,
  useMediaAdmissions: lobbyMocks.admissions,
  useInviteMediaSpaceMember: lobbyMocks.invite,
  useMediaSpaceMembers: lobbyMocks.members,
  useMutateMediaSpaceMember: lobbyMocks.mutateMember,
  useResolveMediaAdmission: lobbyMocks.resolve,
}));

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const spaceID = "c2dc1048-1d90-4c90-ae50-5fb436bfb607";
const roomInstanceID = "c5f918a5-a09e-4f94-9fab-fb0ab5702a4d";
const admissionID = "d48a301d-c468-4f65-8da2-029fc379ee74";
const userID = "f680fd29-c7f1-4083-af9b-52ad1db14ba9";

function queryResult(data: unknown) {
  return {
    data,
    error: null,
    isError: false,
    isFetching: false,
    isPending: false,
    isRefetching: false,
    isSuccess: true,
    refetch: vi.fn(),
  };
}

function mutationResult() {
  return {
    error: null,
    isError: false,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
    variables: undefined,
  };
}

function renderEnglish(node: ReactNode) {
  return render(<I18nProvider initialLanguage="en">{node}</I18nProvider>);
}

afterEach(cleanup);

beforeEach(() => {
  vi.clearAllMocks();
  lobbyMocks.admissions.mockReturnValue(
    queryResult({
      room_instance_id: roomInstanceID,
      room_instance_version: 3,
      items: [
        {
          id: admissionID,
          status: "waiting",
          version: 2,
          display_name: "Student One",
          created_at: "2030-08-03T00:00:00Z",
        },
      ],
    }),
  );
  lobbyMocks.members.mockReturnValue(
    queryResult({
      items: [
        {
          user_id: userID,
          display_name: "Student One",
          status: "active",
          version: 1,
          created_at: "2030-08-03T00:00:00Z",
          updated_at: "2030-08-03T00:00:00Z",
        },
      ],
    }),
  );
  lobbyMocks.resolve.mockReturnValue(mutationResult());
  lobbyMocks.invite.mockReturnValue(mutationResult());
  lobbyMocks.mutateMember.mockReturnValue(mutationResult());
});

describe("MediaLobbyPanel", () => {
  it("renders a privacy-bounded queue and submits an exact admit CAS", () => {
    renderEnglish(
      <MediaLobbyPanel
        enabled
        roomInstanceID={roomInstanceID}
        roomInstanceVersion={3}
        spaceID={spaceID}
        spaceVersion={5}
        tenantID={tenantID}
      />,
    );

    expect(screen.getByRole("heading", { name: "Waiting room" })).toBeVisible();
    expect(screen.getByText("Student One")).toBeVisible();
    expect(document.body).not.toHaveTextContent("student@example.test");
    fireEvent.click(screen.getByRole("button", { name: "Admit Student One" }));

    expect(lobbyMocks.resolve().mutate).toHaveBeenCalledWith(
      {
        action: "admit",
        admissionID,
        input: {
          expected_space_version: 5,
          expected_room_instance_id: roomInstanceID,
          expected_room_instance_version: 3,
          expected_admission_version: 2,
          idempotency_key: "media-admission-admit-fixed-key",
        },
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("requires confirmation before denying a waiting participant", () => {
    renderEnglish(
      <MediaLobbyPanel
        enabled
        roomInstanceID={roomInstanceID}
        roomInstanceVersion={3}
        spaceID={spaceID}
        spaceVersion={5}
        tenantID={tenantID}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Deny Student One" }));
    expect(
      screen.getByRole("heading", { name: "Deny this join request?" }),
    ).toBeVisible();
    expect(lobbyMocks.resolve().mutate).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Confirm denial" }));
    expect(lobbyMocks.resolve().mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        action: "deny",
        admissionID,
        input: expect.objectContaining({ reason_code: "moderator_denied" }),
      }),
      expect.any(Object),
    );
  });

  it("fails closed when moderation authority is revoked mid-session", () => {
    lobbyMocks.resolve.mockReturnValue({
      ...mutationResult(),
      error: new APIRequestError(403),
      isError: true,
    });

    renderEnglish(
      <MediaLobbyPanel
        enabled
        roomInstanceID={roomInstanceID}
        roomInstanceVersion={3}
        spaceID={spaceID}
        spaceVersion={5}
        tenantID={tenantID}
      />,
    );

    expect(
      screen.getByRole("heading", { name: "The waiting room is unavailable" }),
    ).toBeVisible();
    expect(screen.queryByText("Student One")).not.toBeInTheDocument();
  });
});

describe("MediaSpaceInvitePanel", () => {
  it("normalizes an explicit member email without rendering it in the grant list", () => {
    renderEnglish(
      <MediaSpaceInvitePanel
        enabled
        spaceID={spaceID}
        spaceVersion={5}
        tenantID={tenantID}
      />,
    );

    fireEvent.change(
      screen.getByRole("textbox", { name: "Workspace member email" }),
      { target: { value: " Student@Example.Test " } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Invite member" }));

    expect(lobbyMocks.invite().mutate).toHaveBeenCalledWith(
      {
        target_member_email: "student@example.test",
        expected_space_version: 5,
        idempotency_key: "media-member-invite-fixed-key",
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
    expect(screen.getByText("Student One")).toBeVisible();
    expect(document.body).not.toHaveTextContent("student@example.test");
  });

  it("confirms revocation before sending the exact member version", () => {
    renderEnglish(
      <MediaSpaceInvitePanel
        enabled
        spaceID={spaceID}
        spaceVersion={5}
        tenantID={tenantID}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Revoke Student One's access" }),
    );
    expect(
      screen.getByRole("heading", {
        name: "Revoke this member's access?",
      }),
    ).toBeVisible();
    expect(lobbyMocks.mutateMember().mutate).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Confirm revocation" }));
    expect(lobbyMocks.mutateMember().mutate).toHaveBeenCalledWith(
      {
        action: "revoke",
        userID,
        input: {
          expected_member_version: 1,
          expected_space_version: 5,
          idempotency_key: "media-member-revoke-fixed-key",
          reason_code: "owner_revoked",
        },
      },
      expect.any(Object),
    );
  });

  it("keeps an exact-member 404 generic without hiding existing grants", () => {
    lobbyMocks.invite.mockReturnValue({
      ...mutationResult(),
      error: new APIRequestError(404),
      isError: true,
    });

    renderEnglish(
      <MediaSpaceInvitePanel
        enabled
        spaceID={spaceID}
        spaceVersion={5}
        tenantID={tenantID}
      />,
    );

    expect(screen.getByText("Student One")).toBeVisible();
    expect(
      screen.getByText(
        "This account cannot be invited. Check that it is an active member of the workspace.",
      ),
    ).toBeVisible();
  });
});
