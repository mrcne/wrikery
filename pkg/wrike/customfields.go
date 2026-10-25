package wrike

import (
	"context"
	"net/http"
)

// CustomField is a custom field the account defines, see https://developers.wrike.com/api/v4/custom-fields/.
// Type is one of Text, DropDown, Numeric, Currency, Percentage, Date, Duration, Checkbox, Contacts, Multiple,
// LinkToDatabase, CalculatedNumeric and CalculatedDate, a formula field is CalculatedNumeric.
type CustomField struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	SpaceID string `json:"spaceId"` // empty for an account level field, the space that owns the field otherwise
}

// CustomFieldValue is one value a task carries, a string whatever the field's type (https://developers.wrike.com/api/v4/tasks/).
type CustomFieldValue struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// TaskEffort is the Effort field of a task, see the Modify Task reference.
// TotalEffort is in minutes, Mode is Basic, Flexible, FullTime or None.
type TaskEffort struct {
	Mode        string `json:"mode"`
	TotalEffort int    `json:"totalEffort"`
}

// CustomFields lists the custom fields of the account.
func (c *Client) CustomFields(ctx context.Context) ([]CustomField, error) {
	var out []CustomField
	if _, err := c.do(ctx, http.MethodGet, "/customfields", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
