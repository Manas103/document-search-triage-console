export interface Document {
  id: string;
  title: string;
  body: string;
  category: string;
  region: string;
  priority: string;
  source_type: string;
  classification: string;
  status: string;
  created_date: string;
}

export interface SearchResponse {
  results: Document[];
  total: number;
}

export interface SearchFacets {
  q?: string;
  category?: string;
  region?: string;
  priority?: string;
  source_type?: string;
  classification?: string;
  status?: string;
}

export const CATEGORIES = ['Infrastructure', 'Maritime', 'Aviation', 'Cyber', 'Logistics', 'Communications', 'Border', 'Industrial'];
export const REGIONS = Array.from({ length: 12 }, (_, i) => `REGION-${String(i + 1).padStart(2, '0')}`);
export const PRIORITIES = ['LOW', 'MEDIUM', 'HIGH', 'CRITICAL'];
export const SOURCE_TYPES = ['SENSOR', 'FIELD', 'OPEN_SOURCE', 'PARTNER'];
export const CLASSIFICATIONS = ['UNCLASSIFIED', 'CUI', 'RESTRICTED'];
export const STATUSES = ['NEW', 'IN_REVIEW', 'FLAGGED', 'CLOSED'];

// Mirrors the server's internal/auth.allowedTransitions table, used here
// only to decide which triage buttons to show. The server re-checks every
// one of these independently and does not trust this list at all; see
// bypass case 11 through 15 in the backend for proof.
export const NEXT_STATUS_FOR_ROLE: Record<string, Record<string, string | null>> = {
  ANALYST: { NEW: 'IN_REVIEW', IN_REVIEW: 'FLAGGED', FLAGGED: null, CLOSED: null },
  SUPERVISOR: { NEW: 'IN_REVIEW', IN_REVIEW: 'FLAGGED', FLAGGED: 'CLOSED', CLOSED: null },
};
