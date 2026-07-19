import type {
  ClassEnrollmentRole,
  ClassRosterMember,
} from "@tutorhub/api-client";

export type RosterBulkChoice =
  | "suspend"
  | "remove"
  | "role:co_teacher"
  | "role:teaching_assistant"
  | "role:student";

const allBulkChoices: readonly RosterBulkChoice[] = [
  "suspend",
  "remove",
  "role:co_teacher",
  "role:teaching_assistant",
  "role:student",
];

function memberSupportsBulkChoice(
  member: ClassRosterMember,
  choice: RosterBulkChoice,
) {
  if (choice === "suspend") {
    return member.actions.can_suspend;
  }
  if (choice === "remove") {
    return member.actions.can_remove;
  }
  return member.actions.assignable_roles.includes(
    choice.slice("role:".length) as ClassEnrollmentRole,
  );
}

export function intersectRosterBulkChoices(
  members: readonly ClassRosterMember[],
) {
  if (members.length === 0) {
    return [...allBulkChoices];
  }
  return allBulkChoices.filter((choice) =>
    members.every((member) => memberSupportsBulkChoice(member, choice)),
  );
}
