// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../../app/i18n";
import {
  ClassroomModerationControls,
  ClassroomParticipantModerationMenu,
  type ClassroomModerationControlsModel,
} from "./ClassroomModerationControls";

const targetParticipantID = "018f4c7b-9b0a-7a34-8a4c-96d26cb87222";

describe("ClassroomModerationControls", () => {
  afterEach(() => cleanup());

  it("fails closed when the server grants no exact moderation operation", () => {
    const controls = createControls();
    const view = renderModeration(
      <>
        <ClassroomModerationControls controls={controls} />
        <ClassroomParticipantModerationMenu
          controls={controls}
          displayName="Student One"
          isSelf={false}
          participantKey={targetParticipantID}
        />
      </>,
    );

    expect(view.container).toBeEmptyDOMElement();
    expect(controls.onSetRoomLocked).not.toHaveBeenCalled();
    expect(controls.onRemoteMute).not.toHaveBeenCalled();
  });

  it("keeps lock state and success feedback server-owned", async () => {
    const controls = createControls({ canLockRoom: true });
    const view = renderModeration(
      <ClassroomModerationControls controls={controls} />,
    );

    const lock = screen.getByRole("button", { name: "Lock room" });
    expect(lock).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(lock);
    await waitFor(() =>
      expect(controls.onSetRoomLocked).toHaveBeenCalledWith(true),
    );
    expect(lock).toHaveAttribute("aria-pressed", "false");
    expect(
      screen.queryByText("The moderation change was confirmed."),
    ).not.toBeInTheDocument();

    view.rerender(
      moderationElement(
        <ClassroomModerationControls
          controls={createControls({
            canLockRoom: true,
            roomLocked: true,
            providerEffect: {
              status: "pending",
              action: "lock_room",
            },
          })}
        />,
      ),
    );
    expect(screen.getByRole("button", { name: "Unlock room" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(
      screen.getByText(
        "TutorHub accepted the command and is waiting for provider confirmation.",
      ),
    ).toHaveAttribute("role", "status");
  });

  it("requires confirmation for end and keeps the dialog until external apply", async () => {
    const controls = createControls({ canEndRoom: true });
    const view = renderModeration(
      <ClassroomModerationControls controls={controls} />,
    );

    fireEvent.click(screen.getByRole("button", { name: "End room" }));
    expect(
      screen.getByRole("heading", { name: "End this classroom?" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Confirm end" }));
    await waitFor(() => expect(controls.onEndRoom).toHaveBeenCalledTimes(1));
    expect(
      screen.getByRole("heading", { name: "End this classroom?" }),
    ).toBeInTheDocument();

    view.rerender(
      moderationElement(
        <ClassroomModerationControls
          controls={createControls({
            canEndRoom: true,
            providerEffect: { status: "applied", action: "end_room" },
          })}
        />,
      ),
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("heading", { name: "End this classroom?" }),
      ).not.toBeInTheDocument(),
    );
  });

  it("offers exact row actions, remote mute only, and confirms removal", async () => {
    const controls = createControls({
      participantOperations: [
        {
          participantKey: targetParticipantID,
          canPromoteCoHost: true,
          canDemoteCoHost: false,
          canRemoteMute: true,
          canRemove: true,
        },
      ],
    });
    const view = renderModeration(
      <ClassroomParticipantModerationMenu
        controls={controls}
        displayName="Student One"
        isSelf={false}
        participantKey={targetParticipantID}
      />,
    );

    openParticipantMenu();
    expect(
      await screen.findByRole("menuitem", { name: "Promote to co-host" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: "Mute microphone remotely" }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/unmute/i)).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("menuitem", { name: "Mute microphone remotely" }),
    );
    await waitFor(() =>
      expect(controls.onRemoteMute).toHaveBeenCalledWith(targetParticipantID),
    );

    openParticipantMenu();
    fireEvent.click(
      await screen.findByRole("menuitem", { name: "Remove from room" }),
    );
    expect(
      await screen.findByRole("heading", { name: "Remove this participant?" }),
    ).toBeInTheDocument();
    expect(document.body.textContent).not.toContain(targetParticipantID);
    fireEvent.click(screen.getByRole("button", { name: "Confirm removal" }));
    await waitFor(() =>
      expect(controls.onRemoveParticipant).toHaveBeenCalledWith(
        targetParticipantID,
      ),
    );
    expect(
      screen.getByRole("heading", { name: "Remove this participant?" }),
    ).toBeInTheDocument();

    view.rerender(
      moderationElement(
        <ClassroomParticipantModerationMenu
          controls={createControls({
            participantOperations: controls.participantOperations,
            providerEffect: {
              status: "applied",
              action: "remove_participant",
              targetParticipantKey: targetParticipantID,
            },
          })}
          displayName="Student One"
          isSelf={false}
          participantKey={targetParticipantID}
        />,
      ),
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("heading", { name: "Remove this participant?" }),
      ).not.toBeInTheDocument(),
    );
  });

  it("never renders self-moderation even when a malformed projection grants it", () => {
    const controls = createControls({
      participantOperations: [
        {
          participantKey: targetParticipantID,
          canPromoteCoHost: true,
          canDemoteCoHost: true,
          canRemoteMute: true,
          canRemove: true,
        },
      ],
    });
    const view = renderModeration(
      <ClassroomParticipantModerationMenu
        controls={controls}
        displayName="Current user"
        isSelf
        participantKey={targetParticipantID}
      />,
    );
    expect(view.container).toBeEmptyDOMElement();
  });
});

function createControls(
  overrides: Partial<ClassroomModerationControlsModel> = {},
): ClassroomModerationControlsModel {
  return {
    roomLocked: false,
    canLockRoom: false,
    canEndRoom: false,
    participantOperations: [],
    mutationState: { status: "idle" },
    providerEffect: { status: "idle" },
    onSetRoomLocked: vi.fn().mockResolvedValue(undefined),
    onEndRoom: vi.fn().mockResolvedValue(undefined),
    onPromoteCoHost: vi.fn().mockResolvedValue(undefined),
    onDemoteCoHost: vi.fn().mockResolvedValue(undefined),
    onRemoteMute: vi.fn().mockResolvedValue(undefined),
    onRemoveParticipant: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

function renderModeration(node: React.ReactNode) {
  return render(moderationElement(node));
}

function moderationElement(node: React.ReactNode) {
  return <I18nProvider initialLanguage="en">{node}</I18nProvider>;
}

function openParticipantMenu() {
  const trigger = screen.getByRole("button", { name: "Moderate Student One" });
  fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false });
}
