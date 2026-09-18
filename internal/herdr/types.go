package herdr

// Snapshot is the part of session.snapshot this plugin reads.
type Snapshot struct {
	Workspaces []Workspace `json:"workspaces"`
	Tabs       []Tab       `json:"tabs"`
	Panes      []Pane      `json:"panes"`
	Agents     []Agent     `json:"agents"`
}

// Workspace is one Space in the sidebar.
type Workspace struct {
	ID          string            `json:"workspace_id"`
	Label       string            `json:"label"`
	Number      int               `json:"number"`
	AgentStatus string            `json:"agent_status"`
	Tokens      map[string]string `json:"tokens"`
}

// Tab is one tab inside a workspace.
type Tab struct {
	ID          string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Number      int    `json:"number"`
}

// AgentSession is the session reference an integration hook reported.
type AgentSession struct {
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Value  string `json:"value"`
}

// Pane is one terminal.
type Pane struct {
	ID            string `json:"pane_id"`
	WorkspaceID   string `json:"workspace_id"`
	TabID         string `json:"tab_id"`
	CWD           string `json:"cwd"`
	ForegroundCWD string `json:"foreground_cwd"`
}

// Agent is a pane Herdr has recognized as running a coding agent.
type Agent struct {
	PaneID        string        `json:"pane_id"`
	WorkspaceID   string        `json:"workspace_id"`
	TabID         string        `json:"tab_id"`
	Agent         string        `json:"agent"`
	AgentSession  *AgentSession `json:"agent_session"`
	AgentStatus   string        `json:"agent_status"`
	CWD           string        `json:"cwd"`
	ForegroundCWD string        `json:"foreground_cwd"`
	Title         string        `json:"terminal_title_stripped"`
}

// Dir is the directory that best describes the agent's work.
func (a Agent) Dir() string {
	if a.ForegroundCWD != "" {
		return a.ForegroundCWD
	}
	return a.CWD
}

// Dir is the directory that best describes the pane.
func (p Pane) Dir() string {
	if p.ForegroundCWD != "" {
		return p.ForegroundCWD
	}
	return p.CWD
}
