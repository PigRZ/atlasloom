// 关联字段中的地点快捷创建保留父表单，并在成功后把新地点交回原字段。
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useCallback, useState } from "react";
import { EditorSurface } from "../../components/EditorSurface";
import { useConfirm } from "../../components/ConfirmDialog";
import { api } from "../../lib/api";
import { resources } from "../../lib/resourceDefinitions";
import type { RecordRow, SnapshotSummary } from "../../lib/types";
import { RecordForm, type FieldOption } from "./RecordForm";

type PlaceQuickCreateProps = {
  tripId: string;
  targetLabel: string;
  idempotencyKey: string;
  placeTypeOptions: FieldOption[];
  associationDay?: RecordRow;
  onSnapshot: (snapshot: SnapshotSummary) => void;
  onCreated: (option: FieldOption) => void;
  onDirtyChange?: (dirty: boolean) => void;
  onClose: () => void;
};

const placeDefinition = resources.find((resource) => resource.id === "places")!;

export function PlaceQuickCreate({
  tripId,
  targetLabel,
  idempotencyKey,
  placeTypeOptions,
  associationDay,
  onSnapshot,
  onCreated,
  onDirtyChange,
  onClose
}: PlaceQuickCreateProps) {
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const [dirty, setDirty] = useState(false);
  const create = useMutation({
    mutationFn: async ({
      recordKey,
      data
    }: {
      recordKey: string;
      data: Record<string, unknown>;
    }) => {
      if (associationDay) {
        const result = await api.createPlaceForDay(
          tripId,
          associationDay,
          recordKey,
          data,
          idempotencyKey
        );
        return { place: result.place, snapshot: result.snapshot };
      }
      const result = await api.createRecord(
        tripId,
        "places",
        recordKey,
        data,
        idempotencyKey
      );
      return { place: result.record, snapshot: result.snapshot };
    }
  });

  const close = useCallback(() => {
    setDirty(false);
    onDirtyChange?.(false);
    onClose();
  }, [onClose, onDirtyChange]);
  const requestClose = useCallback(async () => {
    if (!dirty || await confirm({
      title: "放弃新增地点？",
      message: "地点表单有未保存的修改，父表单会继续保留。",
      confirmLabel: "放弃新增",
      tone: "danger"
    })) close();
  }, [close, confirm, dirty]);
  const dirtyChanged = useCallback((value: boolean) => {
    setDirty(value);
    onDirtyChange?.(value);
  }, [onDirtyChange]);

  return (
    <EditorSurface
      open
      title="新增地点"
      variant="modal"
      layer="nested"
      dirty={dirty}
      onClose={close}
    >
      <RecordForm
        definition={placeDefinition}
        tripId={tripId}
        options={{ type: placeTypeOptions }}
        contextNote={`创建后会返回当前表单，并自动选为${targetLabel}。`}
        submitLabel="创建并选中"
        pending={create.isPending}
        onDirtyChange={dirtyChanged}
        onCancel={requestClose}
        onSubmit={async (recordKey, data) => {
          const result = await create.mutateAsync({ recordKey, data });
          onSnapshot(result.snapshot);
          await Promise.all([
            queryClient.invalidateQueries({ queryKey: ["records", tripId] }),
            queryClient.invalidateQueries({ queryKey: ["overview", tripId] }),
            queryClient.invalidateQueries({ queryKey: ["audit-logs", tripId] }),
            queryClient.invalidateQueries({ queryKey: ["trips"] })
          ]);
          setDirty(false);
          onDirtyChange?.(false);
          onCreated({
            value: result.place.id,
            label: String(result.place.data.name ?? result.place.recordKey)
          });
          onClose();
        }}
      />
    </EditorSurface>
  );
}
