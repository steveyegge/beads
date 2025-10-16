"""Pydantic models for beads issue tracker types."""

from datetime import datetime
from typing import Literal

from pydantic import BaseModel, Field, field_validator

# Type aliases for issue statuses, types, and dependencies
IssueStatus = Literal["open", "in_progress", "blocked", "closed"]
IssueType = Literal["bug", "feature", "task", "epic", "chore"]
DependencyType = Literal["blocks", "related", "parent-child", "discovered-from"]


class Issue(BaseModel):
    """Issue model matching bd JSON output."""

    id: str
    title: str
    description: str = ""
    design: str | None = None
    acceptance_criteria: str | None = None
    notes: str | None = None
    external_ref: str | None = None
    status: IssueStatus
    priority: int = Field(ge=0, le=4)
    issue_type: IssueType
    created_at: datetime
    updated_at: datetime
    closed_at: datetime | None = None
    assignee: str | None = None
    labels: list[str] = Field(default_factory=list)
    dependencies: list["Issue"] = Field(default_factory=list)
    dependents: list["Issue"] = Field(default_factory=list)

    @field_validator("priority")
    @classmethod
    def validate_priority(cls, v: int) -> int:
        """Validate priority is 0-4."""
        if not 0 <= v <= 4:
            raise ValueError("Priority must be between 0 and 4")
        return v


class Dependency(BaseModel):
    """Dependency relationship model."""

    from_id: str
    to_id: str
    dep_type: DependencyType


class CreateIssueParams(BaseModel):
    """Parameters for creating an issue."""

    title: str
    description: str = ""
    design: str | None = None
    acceptance: str | None = None
    external_ref: str | None = None
    priority: int = Field(default=2, ge=0, le=4)
    issue_type: IssueType = "task"
    assignee: str | None = None
    labels: list[str] = Field(default_factory=list)
    id: str | None = None
    deps: list[str] = Field(default_factory=list)


class UpdateIssueParams(BaseModel):
    """Parameters for updating an issue."""

    issue_id: str
    status: IssueStatus | None = None
    priority: int | None = Field(default=None, ge=0, le=4)
    assignee: str | None = None
    title: str | None = None
    design: str | None = None
    acceptance_criteria: str | None = None
    notes: str | None = None
    external_ref: str | None = None


class CloseIssueParams(BaseModel):
    """Parameters for closing an issue."""

    issue_id: str
    reason: str = "Completed"


class ReopenIssueParams(BaseModel):
    """Parameters for reopening issues."""

    issue_ids: list[str]
    reason: str | None = None


class AddDependencyParams(BaseModel):
    """Parameters for adding a dependency."""

    from_id: str
    to_id: str
    dep_type: DependencyType = "blocks"


class ReadyWorkParams(BaseModel):
    """Parameters for querying ready work."""

    limit: int = Field(default=10, ge=1, le=100)
    priority: int | None = Field(default=None, ge=0, le=4)
    assignee: str | None = None


class ListIssuesParams(BaseModel):
    """Parameters for listing issues."""

    status: IssueStatus | None = None
    priority: int | None = Field(default=None, ge=0, le=4)
    issue_type: IssueType | None = None
    assignee: str | None = None
    limit: int = Field(default=50, ge=1, le=1000)


class ShowIssueParams(BaseModel):
    """Parameters for showing issue details."""

    issue_id: str


class Stats(BaseModel):
    """Beads task statistics."""

    total_issues: int
    open_issues: int
    in_progress_issues: int
    closed_issues: int
    blocked_issues: int
    ready_issues: int
    average_lead_time_hours: float


class BlockedIssue(Issue):
    """Blocked issue with blocking information."""

    blocked_by_count: int
    blocked_by: list[str]


class InitParams(BaseModel):
    """Parameters for initializing bd."""

    prefix: str | None = None


class InitResult(BaseModel):
    """Result from bd init command."""

    database: str
    prefix: str
    message: str
