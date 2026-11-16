import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@tutorhub/ui";
import { useId, useLayoutEffect, useRef, type RefObject } from "react";
import { useI18n } from "../app/i18n";
import {
  SIGN_IN_LEGAL_TABS,
  isSignInLegalTab,
  signInLegalContent,
  type SignInLegalDocument,
  type SignInLegalTab,
} from "./signInLegalContent";

export interface SignInLegalDialogProps {
  activeTab: SignInLegalTab;
  onAccept: () => void;
  onOpenChange: (open: boolean) => void;
  onTabChange: (tab: SignInLegalTab) => void;
  open: boolean;
  /** Element that receives focus once the dialog has closed. */
  returnFocusRef: RefObject<HTMLElement | null>;
}

export function SignInLegalDialog({
  activeTab,
  onAccept,
  onOpenChange,
  onTabChange,
  open,
  returnFocusRef,
}: SignInLegalDialogProps) {
  const { language } = useI18n();
  const copy = signInLegalContent[language];
  const bodyRef = useRef<HTMLDivElement>(null);

  // One always-visible scroll container: Radix keeps inactive panels as hidden
  // nodes, so resetting a panel itself would not survive switching back to it.
  useLayoutEffect(() => {
    if (open && bodyRef.current) bodyRef.current.scrollTop = 0;
  }, [activeTab, open]);

  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogContent
        aria-modal="true"
        className="sign-in-legal"
        closeLabel={copy.closeDialog}
        onCloseAutoFocus={(event) => {
          const target = returnFocusRef.current;
          if (target?.isConnected) {
            event.preventDefault();
            target.focus();
          }
        }}
      >
        <div className="sign-in-legal__header">
          <p className="sign-in-legal__meta">{copy.meta}</p>
          <DialogTitle className="sign-in-legal__title">
            {copy.title}
          </DialogTitle>
          <DialogDescription className="sign-in-legal__description">
            {copy.description}
          </DialogDescription>
        </div>

        <Tabs
          className="sign-in-legal__tabs"
          onValueChange={(value) => {
            if (isSignInLegalTab(value)) onTabChange(value);
          }}
          value={activeTab}
        >
          <TabsList
            aria-label={copy.tabListLabel}
            className="sign-in-legal__tab-list"
          >
            {SIGN_IN_LEGAL_TABS.map((tab) => (
              <TabsTrigger className="sign-in-legal__tab" key={tab} value={tab}>
                {copy.documents[tab].tabLabel}
              </TabsTrigger>
            ))}
          </TabsList>
          <div
            aria-label={copy.documents[activeTab].tabLabel}
            className="sign-in-legal__body"
            ref={bodyRef}
            role="region"
            tabIndex={0}
          >
            {SIGN_IN_LEGAL_TABS.map((tab) => (
              <TabsContent
                className="sign-in-legal__panel"
                key={tab}
                tabIndex={-1}
                value={tab}
              >
                <LegalDocument
                  legalDocument={copy.documents[tab]}
                  highlightsTitle={copy.highlightsTitle}
                />
              </TabsContent>
            ))}
          </div>
        </Tabs>

        <DialogFooter className="sign-in-legal__footer">
          <DialogClose asChild>
            <button
              className="sign-in-legal__button sign-in-legal__button--secondary"
              type="button"
            >
              {copy.close}
            </button>
          </DialogClose>
          <DialogClose asChild>
            <button
              className="sign-in-legal__button sign-in-legal__button--primary"
              onClick={onAccept}
              type="button"
            >
              {copy.accept}
            </button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface LegalDocumentProps {
  legalDocument: SignInLegalDocument;
  highlightsTitle: string;
}

function LegalDocument({ highlightsTitle, legalDocument }: LegalDocumentProps) {
  const highlightsId = useId();
  return (
    <>
      <section
        aria-labelledby={highlightsId}
        className="sign-in-legal__highlights"
      >
        <h3 className="sign-in-legal__highlights-title" id={highlightsId}>
          {highlightsTitle}
        </h3>
        <ul className="sign-in-legal__highlights-list">
          {legalDocument.highlights.map((highlight) => (
            <li key={highlight}>{highlight}</li>
          ))}
        </ul>
      </section>

      {legalDocument.sections.map((section, index) => (
        <section className="sign-in-legal__section" key={section.title}>
          <h3 className="sign-in-legal__section-title">
            <span className="sign-in-legal__section-number">{index + 1}.</span>{" "}
            {section.title}
          </h3>
          {section.paragraphs.map((paragraph) => (
            <p className="sign-in-legal__paragraph" key={paragraph}>
              {paragraph}
            </p>
          ))}
        </section>
      ))}
    </>
  );
}
