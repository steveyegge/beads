package static

import "embed"

// Files exposes the static UI assets bundled with the lightweight shell.
//
//go:embed styles.css app.js event_stream.js navigation.js palette.js status_actions.js delete_issue.js labels.js detail_editor.js quick_create.js saved_views.js shell_state.js multiselect.js htmx_focus.js queue_counts.js shortcut_guard.js theme.js vendor/* images/*
var Files embed.FS
