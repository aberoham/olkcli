package graphapi

import (
	"context"
	"fmt"

	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// Category is a simplified outlook category for output
type Category struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName" untrusted:"true"`
	Color       string `json:"color"`
}

func categoryError(action, target string, err error) error {
	if target != "" {
		return sharedMailboxItemError(action, target, mailboxSettingsGrantHint, err)
	}
	return wrapGraph(err, "%s: %s", action, graphErrorMessage(err))
}

// settingsError wraps a failure from the mailbox settings and inbox rule
// endpoints. The own-mailbox form keeps the note about personal accounts; for
// another mailbox the likelier cause is that Graph does not allow the access.
func settingsError(action, target string, err error) error {
	if target != "" {
		return sharedMailboxItemError(action, target, mailboxSettingsGrantHint, err)
	}
	return enterpriseError(action, err)
}

// ListCategories returns the category list of the target mailbox, or of the
// signed-in user's own mailbox when target is empty.
func (c *Client) ListCategories(ctx context.Context, target string) ([]Category, error) {
	resp, err := c.targetUser(target).Outlook().MasterCategories().Get(ctx, nil)
	if err != nil {
		return nil, categoryError("listing categories", target, err)
	}

	categories := make([]Category, 0, len(resp.GetValue()))
	for _, cat := range resp.GetValue() {
		categories = append(categories, convertCategory(cat))
	}
	return categories, nil
}

func (c *Client) CreateCategory(ctx context.Context, target, name, color string) (*Category, error) {
	if err := c.ensureWritable(); err != nil {
		return nil, err
	}
	cat := models.NewOutlookCategory()
	cat.SetDisplayName(&name)

	if color != "" {
		col, err := models.ParseCategoryColor(color)
		if err != nil {
			return nil, fmt.Errorf("invalid color %q: valid colors are preset0 through preset24, or none", color)
		}
		cat.SetColor(col.(*models.CategoryColor))
	}

	created, err := c.targetUser(target).Outlook().MasterCategories().Post(ctx, cat, nil)
	if err != nil {
		return nil, categoryError("creating category", target, err)
	}

	result := convertCategory(created)
	return &result, nil
}

func (c *Client) DeleteCategory(ctx context.Context, target, categoryID string) error {
	if err := c.ensureWritable(); err != nil {
		return err
	}
	if err := validateID(categoryID, "category ID"); err != nil {
		return err
	}
	err := c.targetUser(target).Outlook().MasterCategories().ByOutlookCategoryId(categoryID).Delete(ctx, nil)
	if err != nil {
		return categoryError("deleting category", target, err)
	}
	return nil
}

func convertCategory(cat models.OutlookCategoryable) Category {
	c := Category{}
	if cat.GetId() != nil {
		c.ID = *cat.GetId()
	}
	if cat.GetDisplayName() != nil {
		c.DisplayName = *cat.GetDisplayName()
	}
	if cat.GetColor() != nil {
		c.Color = cat.GetColor().String()
	}
	return c
}
