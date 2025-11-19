mod types;
mod storage;

use clap::{Parser, Subcommand};
use anyhow::Result;

#[derive(Parser)]
#[command(name = "bd")]
#[command(about = "Beads - A lightweight, dependency-aware issue tracker for AI coding agents", long_about = None)]
struct Cli {
    #[command(subcommand)]
    command: Option<Commands>,

    /// Database path (default: auto-discover .beads/*.db)
    #[arg(long, global = true)]
    db: Option<String>,

    /// Actor name for audit trail
    #[arg(long, global = true)]
    actor: Option<String>,

    /// Output in JSON format
    #[arg(long, global = true)]
    json: bool,
}

#[derive(Subcommand)]
enum Commands {
    /// Initialize a new issue tracker
    Init {
        /// Issue prefix (default: bd)
        #[arg(short, long)]
        prefix: Option<String>,
    },

    /// Create a new issue
    Create {
        /// Issue title
        title: String,

        /// Issue description
        #[arg(short, long)]
        description: Option<String>,

        /// Issue type (bug|feature|task|epic|chore)
        #[arg(short = 't', long, default_value = "task")]
        issue_type: String,

        /// Priority (0-4)
        #[arg(short, long, default_value = "2")]
        priority: i32,
    },

    /// List issues
    List {
        /// Filter by status
        #[arg(short, long)]
        status: Option<String>,

        /// Limit number of results
        #[arg(short, long)]
        limit: Option<i32>,
    },

    /// Show issue details
    Show {
        /// Issue ID
        id: String,
    },

    /// Update an issue
    Update {
        /// Issue ID
        id: String,

        /// Update title
        #[arg(long)]
        title: Option<String>,

        /// Update description
        #[arg(long)]
        description: Option<String>,

        /// Update status
        #[arg(long)]
        status: Option<String>,

        /// Update priority
        #[arg(long)]
        priority: Option<i32>,
    },

    /// Close an issue
    Close {
        /// Issue ID
        id: String,

        /// Reason for closing
        #[arg(short, long)]
        reason: Option<String>,
    },

    /// Export issues to JSONL
    Export {
        /// Output file path
        #[arg(short, long)]
        output: Option<String>,
    },

    /// Import issues from JSONL
    Import {
        /// Input file path
        #[arg(short, long)]
        input: Option<String>,
    },
}

fn main() -> Result<()> {
    env_logger::init();

    let cli = Cli::parse();

    match &cli.command {
        Some(Commands::Init { prefix }) => {
            println!("Initializing bd with prefix: {}", prefix.as_deref().unwrap_or("bd"));
            // TODO: Implement initialization
            Ok(())
        }
        Some(Commands::Create { title, description, issue_type, priority }) => {
            println!("Creating issue: {}", title);
            // TODO: Implement create
            Ok(())
        }
        Some(Commands::List { status, limit }) => {
            println!("Listing issues");
            // TODO: Implement list
            Ok(())
        }
        Some(Commands::Show { id }) => {
            println!("Showing issue: {}", id);
            // TODO: Implement show
            Ok(())
        }
        Some(Commands::Update { id, title, description, status, priority }) => {
            println!("Updating issue: {}", id);
            // TODO: Implement update
            Ok(())
        }
        Some(Commands::Close { id, reason }) => {
            println!("Closing issue: {}", id);
            // TODO: Implement close
            Ok(())
        }
        Some(Commands::Export { output }) => {
            println!("Exporting issues");
            // TODO: Implement export
            Ok(())
        }
        Some(Commands::Import { input }) => {
            println!("Importing issues");
            // TODO: Implement import
            Ok(())
        }
        None => {
            println!("No command specified. Use --help for usage information.");
            Ok(())
        }
    }
}
