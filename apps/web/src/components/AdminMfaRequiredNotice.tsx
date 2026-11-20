import { Button } from "@tutorhub/ui";
import { Settings, ShieldCheck } from "lucide-react";
import { Link } from "react-router";
import { useI18n } from "../app/i18n";
import "./AdminMfaRequiredNotice.css";

interface AdminMfaRequiredNoticeProps {
  /** Opens the AAL2 step-up; the refused workspace action runs again after. */
  onVerify: () => void;
  /** Hidden on the Settings page itself. */
  showSettingsLink?: boolean;
}

/**
 * AUTH-09B: 403 `auth_admin_mfa_required` — an `org_admin` workspace needs
 * an AAL2 session. Offers to verify in place, or to set up a second factor
 * in Settings (reachable without an active workspace).
 */
export function AdminMfaRequiredNotice({
  onVerify,
  showSettingsLink = true,
}: AdminMfaRequiredNoticeProps) {
  const { t } = useI18n();
  return (
    <div className="admin-mfa-notice" role="alert">
      <p className="admin-mfa-notice__title">
        {t("workspace.adminMfaRequiredTitle")}
      </p>
      <p>{t("workspace.adminMfaRequired")}</p>
      <div className="admin-mfa-notice__actions">
        <Button leadingIcon={<ShieldCheck />} onClick={onVerify} size="sm">
          {t("workspace.adminMfaVerify")}
        </Button>
        {showSettingsLink && (
          <Link
            className="th-button th-button--secondary th-button--sm admin-mfa-notice__link"
            to="/app/settings"
          >
            <span aria-hidden="true" className="th-button__icon">
              <Settings />
            </span>
            <span>{t("workspace.adminMfaSetup")}</span>
          </Link>
        )}
      </div>
    </div>
  );
}
