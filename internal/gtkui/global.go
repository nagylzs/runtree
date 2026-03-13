package gtkui

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/rt"
)

const ApplicationId = "com.github.nagylzs.runtree.gui"

var RunTree *rt.Tree = nil
var Application *RunTreeApp = nil
var GtkApplication *gtk.Application = nil
