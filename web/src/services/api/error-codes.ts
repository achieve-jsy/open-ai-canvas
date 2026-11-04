/** Stable API reasons; never infer these from localized messages. */
export const ADVERTISING_REASONS = {
    revisionConflict: "advertising_revision_conflict",
    invalidDocument: "advertising_invalid_document",
    resourceUnavailable: "advertising_resource_unavailable",
    resourceInUse: "advertising_resource_in_use",
    activeTasks: "advertising_active_tasks",
} as const;
