import type { MediaSpaceSource } from "@tutorhub/api-client";
import { Button } from "@tutorhub/ui";
import { Video } from "lucide-react";
import { useNavigate } from "react-router";
import {
  MediaSpaceNotReadyError,
  useLaunchMediaSpace,
} from "../app/mediaSpaces";
import { useI18n } from "../app/i18n";

interface MediaSpaceLaunchActionProps {
  canStart: boolean;
  source: MediaSpaceSource;
  tenantID: string | undefined;
}

export function MediaSpaceLaunchAction({
  canStart,
  source,
  tenantID,
}: MediaSpaceLaunchActionProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const launch = useLaunchMediaSpace(tenantID);

  return (
    <div className="media-space-launch-action">
      <Button
        disabled={!tenantID}
        leadingIcon={<Video />}
        loading={launch.isPending}
        loadingLabel={t("media.launch.opening")}
        onClick={() => {
          launch.mutate(
            { canStart, source },
            {
              onSuccess: (space) =>
                void navigate(`/app/media/spaces/${space.id}/prejoin`),
            },
          );
        }}
        size="sm"
        variant={canStart ? "primary" : "secondary"}
      >
        {canStart ? t("media.launch.start") : t("media.launch.join")}
      </Button>
      {launch.isError && (
        <span className="media-space-launch-action__error" role="alert">
          {launch.error instanceof MediaSpaceNotReadyError
            ? t("media.launch.notReady")
            : t("media.launch.error")}
        </span>
      )}
    </div>
  );
}
