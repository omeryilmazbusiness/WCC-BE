-- +goose Up
-- Epic 17: Hardening — list/dashboard composite indexes + pagination-friendly paths (T-212)

-- Leads: owner queue sorted by recency within stage
CREATE INDEX IF NOT EXISTS idx_leads_branch_owner_updated
    ON leads(branch_id, owner_id, updated_at DESC);

-- Bookings: branch list by updated_at (manager/employee workspaces)
CREATE INDEX IF NOT EXISTS idx_bookings_branch_updated
    ON bookings(branch_id, updated_at DESC);

-- Customers: branch list by created_at for paged directories
CREATE INDEX IF NOT EXISTS idx_customers_branch_created
    ON customers(branch_id, created_at DESC);

-- Tasks: assignee open queue (employee home)
CREATE INDEX IF NOT EXISTS idx_tasks_assignee_due_open
    ON tasks(assignee_id, due_at)
    WHERE status IN ('open', 'in_progress');

-- Payments: finance queues by booking + created_at (payments have no branch_id)
CREATE INDEX IF NOT EXISTS idx_payments_booking_created
    ON payments(booking_id, created_at DESC);

-- Conversations: unanswered inbox sort (already has unanswered_since; reinforce branch+updated)
CREATE INDEX IF NOT EXISTS idx_conversations_branch_updated
    ON conversations(branch_id, updated_at DESC);

-- Audit: branch-scoped timeline paging
CREATE INDEX IF NOT EXISTS idx_audit_events_branch_created
    ON audit_events(branch_id, created_at DESC)
    WHERE branch_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_audit_events_branch_created;
DROP INDEX IF EXISTS idx_conversations_branch_updated;
DROP INDEX IF EXISTS idx_payments_booking_created;
DROP INDEX IF EXISTS idx_tasks_assignee_due_open;
DROP INDEX IF EXISTS idx_customers_branch_created;
DROP INDEX IF EXISTS idx_bookings_branch_updated;
DROP INDEX IF EXISTS idx_leads_branch_owner_updated;
