-- The members of the user groups, with the contacts: a mention of a group the user is in is a mention of the user in the inbox.
CREATE TABLE group_members (
    group_id   TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    contact_id TEXT NOT NULL,
    PRIMARY KEY (group_id, contact_id)
);
CREATE INDEX idx_group_members_contact ON group_members(contact_id);
