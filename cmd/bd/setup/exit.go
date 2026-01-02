package setup

import "os"

// setupExit is used by setup commands to exit the process. Tests can stub this.
var setupExit = os.Exit
