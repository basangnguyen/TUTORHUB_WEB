/* eslint-disable react-refresh/only-export-components -- This context module intentionally exports its hook and session contract. */

import { createContext, useContext, type PropsWithChildren } from "react";

export interface DemoSession {
  displayName: string;
  role: "teacher" | "student";
}

export const demoSession: DemoSession = {
  displayName: "TutorHub Preview",
  role: "teacher",
};

const DemoSessionContext = createContext<DemoSession | null | undefined>(
  undefined,
);

export function DemoSessionProvider({
  children,
  session,
}: PropsWithChildren<{ session: DemoSession | null }>) {
  return (
    <DemoSessionContext.Provider value={session}>
      {children}
    </DemoSessionContext.Provider>
  );
}

export function useDemoSession() {
  const session = useContext(DemoSessionContext);

  if (session === undefined) {
    throw new Error("useDemoSession must be used inside DemoSessionProvider.");
  }

  return session;
}
