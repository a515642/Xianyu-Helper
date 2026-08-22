package db

import (
	"context"
	"xianyu-go/internal/deliverytemplate"
)

func (a *AutomationRules) loadTemplateAction(ctx context.Context, action *AutomationAction) error {
	rows, err := a.DB.QueryContext(ctx, `SELECT b.variable_key,b.card_id,COALESCE(c.name,''),b.delivery_count
		FROM automation_action_template_bindings b LEFT JOIN cards c ON c.id=b.card_id
		WHERE b.action_id=? ORDER BY b.id`, action.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	action.TemplateBindings = []DeliveryTemplateBinding{}
	for rows.Next() {
		var binding DeliveryTemplateBinding
		if err := rows.Scan(&binding.VariableKey, &binding.CardID, &binding.CardName, &binding.DeliveryCount); err != nil {
			return err
		}
		action.TemplateBindings = append(action.TemplateBindings, binding)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var messages []string
	messageRows, err := a.DB.QueryContext(ctx, `SELECT content FROM delivery_template_messages WHERE template_id=? ORDER BY sort_order,id`, action.DeliveryTemplateID)
	if err != nil {
		return err
	}
	defer messageRows.Close()
	for messageRows.Next() {
		var content string
		if err := messageRows.Scan(&content); err != nil {
			return err
		}
		messages = append(messages, content)
	}
	if err := messageRows.Err(); err != nil {
		return err
	}
	parsed, err := deliverytemplate.Parse(messages)
	if err != nil {
		return err
	}
	action.TemplateMessages = parsed.Messages
	action.TemplateKeys = parsed.Keys
	return nil
}
