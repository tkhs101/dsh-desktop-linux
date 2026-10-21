//go:build linux

package main

// The tray icon is desktop integration the webview wrapper does not offer: it
// hands out a window, not a status icon. GTK is already linked, so the menu and
// the close-button hook live here -- and the indicator itself is loaded at run
// time, because the library is optional. A machine without it keeps working
// exactly as before: closing the window quits.
//
// The library has two names in the wild. Ayatana's fork is the maintained one
// and the one modern distributions ship; the original libappindicator is what
// older Fedora/RHEL still package. Both export the same entry points, so the
// same few symbols drive either.

/*
#cgo pkg-config: gtk+-3.0
#cgo LDFLAGS: -ldl
#include <gtk/gtk.h>
#include <gdk/gdk.h>
#include <dlfcn.h>
#include <stddef.h>

// AppIndicator is opaque on purpose: only the four calls below are ever made on
// it, so the header is not needed and neither library has to be a build
// dependency.
typedef struct _AppIndicator AppIndicator;

static AppIndicator *(*dsh_ind_new)(const char *, const char *, int);
static void (*dsh_ind_status)(AppIndicator *, int);
static void (*dsh_ind_menu)(AppIndicator *, GtkMenu *);
static void (*dsh_ind_title)(AppIndicator *, const char *);

// APP_INDICATOR_CATEGORY_APPLICATION_STATUS and APP_INDICATOR_STATUS_ACTIVE.
enum { DSH_IND_CATEGORY_APP_STATUS = 0, DSH_IND_STATUS_ACTIVE = 1 };

static AppIndicator *dsh_indicator;
static void *dsh_ind_lib;

static void dsh_tray_forget(void) {
  dsh_ind_new = NULL;
  dsh_ind_status = NULL;
  dsh_ind_menu = NULL;
  dsh_ind_title = NULL;
}

// dsh_tray_load binds the indicator entry points from whichever library is
// installed. set_title arrived late in the original library's life, so it is
// optional; the other three are what an indicator needs to exist at all.
static int dsh_tray_load(void) {
  static const char *names[] = {
      "libayatana-appindicator3.so.1",
      "libayatana-appindicator3.so",
      "libappindicator3.so.1",
      "libappindicator3.so",
  };
  if (dsh_ind_new && dsh_ind_status && dsh_ind_menu) return 1;

  for (size_t i = 0; i < sizeof(names) / sizeof(names[0]); i++) {
    void *lib = dlopen(names[i], RTLD_NOW | RTLD_LOCAL);
    if (!lib) continue;

    dsh_ind_new = (AppIndicator *(*)(const char *, const char *, int))dlsym(lib, "app_indicator_new");
    dsh_ind_status = (void (*)(AppIndicator *, int))dlsym(lib, "app_indicator_set_status");
    dsh_ind_menu = (void (*)(AppIndicator *, GtkMenu *))dlsym(lib, "app_indicator_set_menu");
    dsh_ind_title = (void (*)(AppIndicator *, const char *))dlsym(lib, "app_indicator_set_title");

    if (dsh_ind_new && dsh_ind_status && dsh_ind_menu) {
      dsh_ind_lib = lib;
      return 1;
    }
    dsh_tray_forget();
    dlclose(lib);
  }
  return 0;
}

// dsh_tray_show brings the window back from the tray. present() also raises it
// and gives it focus, which is what a click on a status icon is asking for.
static void dsh_tray_show(GtkWidget *win) {
  gtk_widget_show(win);
  gtk_window_deiconify(GTK_WINDOW(win));
  gtk_window_present(GTK_WINDOW(win));
}

// dsh_tray_show_window is the same action for Go: a second launch while the
// window sits in the tray means "surface it", not "report a lock conflict".
static void dsh_tray_show_window(GtkWidget *win) {
  if (win == NULL) return;
  dsh_tray_show(win);
}

static void dsh_tray_indicator_clicked(AppIndicator *ind, gpointer data) {
  dsh_tray_show(GTK_WIDGET(data));
}

// The restart actions. C owns the menu and the confirmation; the work itself --
// reloading the page, respawning the host, replacing this process -- belongs to
// Go, so the numbers cross the cgo bridge to tray_bridge_linux.go.
enum { DSH_ACTION_FRONTEND = 1, DSH_ACTION_BACKEND = 2, DSH_ACTION_RESTART = 3 };

extern void dshTrayAction(int action);

// dsh_tray_confirm asks before an action that loses something: the host state
// for a backend restart, the whole process for a full one. Cancel is the
// default, and the dialog is parented to the window so it cannot get lost
// behind it.
static int dsh_tray_confirm(GtkWidget *win, int action) {
  // The menu is reachable while the window is hidden in the tray, and a modal
  // dialog over a hidden window would look like a hang: bring it back first.
  dsh_tray_show(win);

  const char *title;
  const char *text;
  if (action == DSH_ACTION_BACKEND) {
    title = "仅重启后端？";
    text = "将停止并重新启动 Harness 后端进程，窗口随后加载新地址。前端当前的页面状态会丢失。";
  } else {
    title = "完全重启？";
    text = "将退出当前窗口并重新启动整个应用：前端与后端都会重启。";
  }

  GtkWidget *dialog = gtk_message_dialog_new(
      GTK_WINDOW(win), GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
      GTK_MESSAGE_QUESTION, GTK_BUTTONS_NONE, "%s", title);
  gtk_window_set_title(GTK_WINDOW(dialog), title);
  gtk_message_dialog_format_secondary_text(GTK_MESSAGE_DIALOG(dialog), "%s", text);
  gtk_dialog_add_button(GTK_DIALOG(dialog), "取消", GTK_RESPONSE_CANCEL);
  gtk_dialog_add_button(GTK_DIALOG(dialog), "重启", GTK_RESPONSE_ACCEPT);
  gtk_dialog_set_default_response(GTK_DIALOG(dialog), GTK_RESPONSE_CANCEL);

  gint response = gtk_dialog_run(GTK_DIALOG(dialog));
  gtk_widget_destroy(dialog);
  return response == GTK_RESPONSE_ACCEPT;
}

// Reloading the page loses nothing that the page cannot rebuild, so it is the
// one action that runs on a single click.
static void dsh_tray_frontend_clicked(GtkMenuItem *item, gpointer data) {
  dshTrayAction(DSH_ACTION_FRONTEND);
}

static void dsh_tray_backend_clicked(GtkMenuItem *item, gpointer data) {
  if (dsh_tray_confirm(GTK_WIDGET(data), DSH_ACTION_BACKEND)) {
    dshTrayAction(DSH_ACTION_BACKEND);
  }
}

static void dsh_tray_restart_clicked(GtkMenuItem *item, gpointer data) {
  if (dsh_tray_confirm(GTK_WIDGET(data), DSH_ACTION_RESTART)) {
    dshTrayAction(DSH_ACTION_RESTART);
  }
}

// Quit stays in C because it is one GTK call: the loop Run() is blocked on is
// the thing that has to end, and gtk_main_quit is exactly what Terminate
// dispatches. Run() then unwinds the host process and the profile lock as usual.
static void dsh_tray_quit_clicked(GtkMenuItem *item, gpointer data) {
  gtk_main_quit();
}

// The close button hides instead of destroying the window. Returning TRUE stops
// GTK's default handler, so the window survives and the loop keeps running.
static gboolean dsh_tray_close(GtkWidget *win, GdkEvent *event, gpointer data) {
  gtk_widget_hide(win);
  return TRUE;
}

static void dsh_tray_add(GtkWidget *menu, const char *label, GCallback cb, GtkWidget *win) {
  GtkWidget *item = gtk_menu_item_new_with_label(label);
  g_signal_connect(G_OBJECT(item), "activate", cb, win);
  gtk_menu_shell_append(GTK_MENU_SHELL(menu), item);
}

// dsh_tray_start puts the icon in the tray and hooks the close button. It
// returns 0 when no indicator library could be loaded, and changes nothing in
// that case -- no tray icon would also mean no way back to a hidden window.
static int dsh_tray_start(GtkWidget *win, const char *icon, const char *title) {
  if (win == NULL || !dsh_tray_load()) return 0;

  GtkWidget *menu = gtk_menu_new();
  dsh_tray_add(menu, "仅重启前端", G_CALLBACK(dsh_tray_frontend_clicked), win);
  dsh_tray_add(menu, "仅重启后端", G_CALLBACK(dsh_tray_backend_clicked), win);
  dsh_tray_add(menu, "完全重启", G_CALLBACK(dsh_tray_restart_clicked), win);
  gtk_menu_shell_append(GTK_MENU_SHELL(menu), gtk_separator_menu_item_new());
  dsh_tray_add(menu, "退出", G_CALLBACK(dsh_tray_quit_clicked), win);
  gtk_widget_show_all(menu);

  dsh_indicator = dsh_ind_new("dsh-desktop", icon, DSH_IND_CATEGORY_APP_STATUS);
  if (dsh_indicator == NULL) return 0;
  dsh_ind_menu(dsh_indicator, GTK_MENU(menu));
  if (dsh_ind_title != NULL) dsh_ind_title(dsh_indicator, title);
  dsh_ind_status(dsh_indicator, DSH_IND_STATUS_ACTIVE);

  // A left click on the icon is "show me the window" on the hosts that send it;
  // the menu carries the same action for the hosts that do not.
  if (g_signal_lookup("activate", G_OBJECT_TYPE(dsh_indicator)) != 0) {
    g_signal_connect(G_OBJECT(dsh_indicator), "activate", G_CALLBACK(dsh_tray_indicator_clicked), win);
  }

  g_signal_connect(G_OBJECT(win), "delete-event", G_CALLBACK(dsh_tray_close), NULL);
  return 1;
}

// dsh_icon_in_theme asks the desktop whether the installed icon set already has
// this name; when it does, the indicator should use the name so the host picks
// the size it wants.
static int dsh_icon_in_theme(const char *name) {
  return gtk_icon_theme_has_icon(gtk_icon_theme_get_default(), name) ? 1 : 0;
}
*/
import "C"

import (
	"os"
	"path/filepath"
	"unsafe"
)

// trayIconName is the icon identity the installer publishes into the hicolor
// theme (scripts/install.sh writes dsh-desktop.png at every size).
const trayIconName = "dsh-desktop"

// startTray adds the status icon and makes the close button hide the window
// instead of ending it. It reports false when no indicator library is installed,
// in which case the app keeps its old meaning for closing.
func startTray(win unsafe.Pointer, title string) bool {
	if win == nil {
		return false
	}
	cIcon := C.CString(trayIcon())
	defer C.free(unsafe.Pointer(cIcon))
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	return C.dsh_tray_start((*C.GtkWidget)(win), cIcon, cTitle) != 0
}

// showWindow brings a hidden window back to the screen. It is the tray menu's
// action, exposed to the rest of the app: once closing means hiding, starting
// the app a second time should surface the window rather than complain that one
// is already open.
func showWindow(win unsafe.Pointer) {
	if win == nil {
		return
	}
	C.dsh_tray_show_window((*C.GtkWidget)(win))
}

// trayIcon is what the indicator should draw. The theme name is best when the
// icon is installed, but a tarball or a source checkout has no theme entry, so
// the PNG next to the binary stands in; the bare name is the last resort so the
// host can draw whatever it has.
func trayIcon() string {
	cName := C.CString(trayIconName)
	defer C.free(unsafe.Pointer(cName))
	if C.dsh_icon_in_theme(cName) != 0 {
		return trayIconName
	}

	exe, err := os.Executable()
	if err != nil {
		return trayIconName
	}
	for _, rel := range []string{
		filepath.Join("assets", "icons", "icon-48.png"),
		filepath.Join("..", "share", "icons", "hicolor", "48x48", "apps", trayIconName+".png"),
	} {
		path := filepath.Join(filepath.Dir(exe), rel)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return trayIconName
}
