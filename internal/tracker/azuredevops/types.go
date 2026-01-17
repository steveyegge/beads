// Package azuredevops provides an Azure DevOps integration plugin for the tracker framework.
package azuredevops

import (
	"time"
)

// API constants
const (
	DefaultTimeout = 30 * time.Second
	MaxPageSize    = 200
	APIVersion     = "7.0"
)

// WorkItem represents an Azure DevOps work item.
type WorkItem struct {
	ID     int               `json:"id"`
	Rev    int               `json:"rev"`
	URL    string            `json:"url"`
	Fields WorkItemFields    `json:"fields"`
	Links  *WorkItemLinks    `json:"_links,omitempty"`
}

// WorkItemFields contains the work item field values.
type WorkItemFields struct {
	Title           string     `json:"System.Title"`
	Description     string     `json:"System.Description"`
	State           string     `json:"System.State"`
	WorkItemType    string     `json:"System.WorkItemType"`
	Priority        int        `json:"Microsoft.VSTS.Common.Priority,omitempty"` // 1=High, 2=Medium, 3=Low, 4=Backlog
	Severity        string     `json:"Microsoft.VSTS.Common.Severity,omitempty"`
	AssignedTo      *Identity  `json:"System.AssignedTo,omitempty"`
	CreatedBy       *Identity  `json:"System.CreatedBy,omitempty"`
	CreatedDate     string     `json:"System.CreatedDate"`
	ChangedDate     string     `json:"System.ChangedDate"`
	ClosedDate      string     `json:"Microsoft.VSTS.Common.ClosedDate,omitempty"`
	ResolvedDate    string     `json:"Microsoft.VSTS.Common.ResolvedDate,omitempty"`
	Tags            string     `json:"System.Tags,omitempty"` // Semicolon-separated
	AreaPath        string     `json:"System.AreaPath"`
	IterationPath   string     `json:"System.IterationPath"`
	Parent          int        `json:"System.Parent,omitempty"`
}

// Identity represents an Azure DevOps user identity.
type Identity struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	UniqueName  string `json:"uniqueName"`
	ImageURL    string `json:"imageUrl,omitempty"`
}

// WorkItemLinks contains hypermedia links.
type WorkItemLinks struct {
	Self     Link `json:"self"`
	WorkItem Link `json:"workItemComments,omitempty"`
	HTML     Link `json:"html"`
}

// Link is a hypermedia link.
type Link struct {
	Href string `json:"href"`
}

// WorkItemRelation represents a link between work items.
type WorkItemRelation struct {
	Rel        string                 `json:"rel"`
	URL        string                 `json:"url"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// WIQLQueryRequest is the request body for WIQL queries.
type WIQLQueryRequest struct {
	Query string `json:"query"`
}

// WIQLQueryResponse is the response from a WIQL query.
type WIQLQueryResponse struct {
	QueryType        string            `json:"queryType"`
	QueryResultType  string            `json:"queryResultType"`
	AsOf             string            `json:"asOf"`
	Columns          []Column          `json:"columns"`
	WorkItems        []WorkItemRef     `json:"workItems"`
	WorkItemRelations []WorkItemRelRef `json:"workItemRelations,omitempty"`
}

// Column describes a column in WIQL results.
type Column struct {
	ReferenceName string `json:"referenceName"`
	Name          string `json:"name"`
	URL           string `json:"url"`
}

// WorkItemRef is a reference to a work item in WIQL results.
type WorkItemRef struct {
	ID  int    `json:"id"`
	URL string `json:"url"`
}

// WorkItemRelRef is a reference with relation info.
type WorkItemRelRef struct {
	Source *WorkItemRef `json:"source,omitempty"`
	Target *WorkItemRef `json:"target"`
	Rel    string       `json:"rel,omitempty"`
}

// WorkItemBatchResponse is the response from batch get.
type WorkItemBatchResponse struct {
	Count int        `json:"count"`
	Value []WorkItem `json:"value"`
}

// CreateWorkItemRequest represents an operation for creating/updating work items.
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
	From  string      `json:"from,omitempty"`
}

// WorkItemCreateResponse is the response from creating a work item.
type WorkItemCreateResponse = WorkItem
