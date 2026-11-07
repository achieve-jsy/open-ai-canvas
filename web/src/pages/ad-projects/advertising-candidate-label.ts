// Advisory display tag only; actions always use the full server ID, available in details.
export const advertisingCandidateLabel = (id: string) => "C-" + (id.length > 16 ? id.slice(0, 8) + id.slice(-8) : id);
