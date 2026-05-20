package domain

import "context"

type LabelSQLRepository interface {
	Insert(ctx context.Context, issueID, label, actor string) error
	List(ctx context.Context, issueID string) ([]string, error)
	ListByIssueIDs(ctx context.Context, issueIDs []string) (map[string][]string, error)
}

type LabelUseCase interface {
	AddLabel(ctx context.Context, issueID, label, actor string) error
	GetLabels(ctx context.Context, issueID string) ([]string, error)
	InheritFromParent(ctx context.Context, childID, parentID, actor string, skipExisting []string) ([]string, error)
}

type LabelQueryUseCase interface {
	GetLabels(ctx context.Context, issueID string) ([]string, error)
	GetLabelsForIssues(ctx context.Context, issueIDs []string) (map[string][]string, error)
}

func NewLabelUseCase(labelRepo LabelSQLRepository) LabelUseCase {
	return nil
}

func NewLabelQueryUseCase(labelRepo LabelSQLRepository) LabelQueryUseCase {
	return nil
}
