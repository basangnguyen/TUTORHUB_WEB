export function cx(
  ...values: ReadonlyArray<string | false | null | undefined>
) {
  return values.filter(Boolean).join(" ");
}
