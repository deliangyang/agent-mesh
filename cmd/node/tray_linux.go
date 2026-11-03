//go:build linux

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	sniIface     = "org.kde.StatusNotifierItem"
	sniPath      = "/StatusNotifierItem"
	watcherName  = "org.kde.StatusNotifierWatcher"
	watcherPath  = "/StatusNotifierWatcher"
	menuIface    = "com.canonical.dbusmenu"
	menuPath     = "/MenuBar"
	sniIntrospect = `
<node>
  <interface name="org.kde.StatusNotifierItem">
    <method name="Activate"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
    <method name="SecondaryActivate"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
    <method name="ContextMenu"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
    <method name="Scroll"><arg name="delta" type="i" direction="in"/><arg name="orientation" type="s" direction="in"/></method>
    <property name="Category" type="s" access="read"/>
    <property name="Id" type="s" access="read"/>
    <property name="Title" type="s" access="read"/>
    <property name="Status" type="s" access="read"/>
    <property name="IconName" type="s" access="read"/>
    <property name="IconPixmap" type="a(iiay)" access="read"/>
    <property name="ItemIsMenu" type="b" access="read"/>
    <property name="Menu" type="o" access="read"/>
  </interface>
` + introspect.IntrospectDataString + prop.IntrospectDataString + `</node>`
	menuIntrospect = `
<node>
  <interface name="com.canonical.dbusmenu">
    <method name="GetLayout">
      <arg name="parentId" type="i" direction="in"/>
      <arg name="recursionDepth" type="i" direction="in"/>
      <arg name="propertyNames" type="as" direction="in"/>
      <arg name="revision" type="u" direction="out"/>
      <arg name="layout" type="(ia{sv}av)" direction="out"/>
    </method>
    <method name="GetGroupProperties">
      <arg name="ids" type="ai" direction="in"/>
      <arg name="propertyNames" type="as" direction="in"/>
      <arg name="properties" type="a(ia{sv})" direction="out"/>
    </method>
    <method name="GetProperty">
      <arg name="id" type="i" direction="in"/>
      <arg name="name" type="s" direction="in"/>
      <arg name="value" type="v" direction="out"/>
    </method>
    <method name="Event">
      <arg name="id" type="i" direction="in"/>
      <arg name="eventId" type="s" direction="in"/>
      <arg name="data" type="v" direction="in"/>
      <arg name="timestamp" type="u" direction="in"/>
    </method>
    <method name="AboutToShow"><arg name="id" type="i" direction="in"/><arg name="needUpdate" type="b" direction="out"/></method>
    <property name="Version" type="u" access="read"/>
    <property name="TextDirection" type="s" access="read"/>
    <property name="Status" type="s" access="read"/>
    <property name="IconThemePath" type="as" access="read"/>
  </interface>
` + introspect.IntrospectDataString + prop.IntrospectDataString + `</node>`
)

type menuLayout struct {
	ID         int32
	Properties map[string]dbus.Variant
	Children   []dbus.Variant
}

type linuxSNI struct {
	uiURL  string
	cancel context.CancelFunc
}

func (s *linuxSNI) Activate(x, y int32) *dbus.Error {
	_, _ = x, y
	openUI(s.uiURL)
	return nil
}

func (s *linuxSNI) SecondaryActivate(x, y int32) *dbus.Error {
	return s.Activate(x, y)
}

func (s *linuxSNI) ContextMenu(x, y int32) *dbus.Error {
	_, _ = x, y
	return nil
}

func (s *linuxSNI) Scroll(delta int32, orientation string) *dbus.Error {
	_, _ = delta, orientation
	return nil
}

type linuxMenu struct {
	uiURL    string
	cancel   context.CancelFunc
	open     string
	settings string
	quit     string
}

func (m *linuxMenu) GetLayout(parentID, recursionDepth int32, propertyNames []string) (uint32, menuLayout, *dbus.Error) {
	_, _ = recursionDepth, propertyNames
	if parentID != 0 {
		return 1, menuLayout{ID: parentID, Properties: map[string]dbus.Variant{}}, nil
	}
	openItem := menuLayout{
		ID: 1,
		Properties: map[string]dbus.Variant{
			"label":       dbus.MakeVariant(m.open),
			"enabled":     dbus.MakeVariant(true),
			"visible":     dbus.MakeVariant(true),
			"type":        dbus.MakeVariant("standard"),
			"interactive": dbus.MakeVariant(true),
		},
	}
	settingsItem := menuLayout{
		ID: 2,
		Properties: map[string]dbus.Variant{
			"label":       dbus.MakeVariant(m.settings),
			"enabled":     dbus.MakeVariant(true),
			"visible":     dbus.MakeVariant(true),
			"type":        dbus.MakeVariant("standard"),
			"interactive": dbus.MakeVariant(true),
		},
	}
	quitItem := menuLayout{
		ID: 3,
		Properties: map[string]dbus.Variant{
			"label":       dbus.MakeVariant(m.quit),
			"enabled":     dbus.MakeVariant(true),
			"visible":     dbus.MakeVariant(true),
			"type":        dbus.MakeVariant("standard"),
			"interactive": dbus.MakeVariant(true),
		},
	}
	root := menuLayout{
		ID: 0,
		Properties: map[string]dbus.Variant{
			"children-display": dbus.MakeVariant("submenu"),
		},
		Children: []dbus.Variant{
			dbus.MakeVariant(openItem),
			dbus.MakeVariant(settingsItem),
			dbus.MakeVariant(quitItem),
		},
	}
	return 1, root, nil
}

func (m *linuxMenu) GetGroupProperties(ids []int32, propertyNames []string) ([]struct {
	ID         int32
	Properties map[string]dbus.Variant
}, *dbus.Error) {
	_, rev, err := m.GetLayout(0, -1, propertyNames)
	if err != nil {
		return nil, err
	}
	out := make([]struct {
		ID         int32
		Properties map[string]dbus.Variant
	}, 0, len(ids))
	items := map[int32]map[string]dbus.Variant{
		0: rev.Properties,
		1: nil,
		2: nil,
		3: nil,
	}
	for _, child := range rev.Children {
		var item menuLayout
		if child.Store(&item) != nil {
			continue
		}
		items[item.ID] = item.Properties
	}
	for _, id := range ids {
		if props, ok := items[id]; ok && props != nil {
			out = append(out, struct {
				ID         int32
				Properties map[string]dbus.Variant
			}{ID: id, Properties: props})
		}
	}
	return out, nil
}

func (m *linuxMenu) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	_, layout, err := m.GetLayout(0, -1, nil)
	if err != nil {
		return dbus.Variant{}, err
	}
	lookup := map[int32]map[string]dbus.Variant{0: layout.Properties}
	for _, child := range layout.Children {
		var item menuLayout
		if child.Store(&item) != nil {
			continue
		}
		lookup[item.ID] = item.Properties
	}
	props := lookup[id]
	if props == nil {
		return dbus.MakeVariant(""), nil
	}
	if v, ok := props[name]; ok {
		return v, nil
	}
	return dbus.MakeVariant(""), nil
}

func (m *linuxMenu) Event(id int32, eventID string, data dbus.Variant, timestamp uint32) *dbus.Error {
	_, _ = data, timestamp
	if eventID != "clicked" {
		return nil
	}
	switch id {
	case 1:
		openUI(m.uiURL)
	case 2:
		openUIAt(m.uiURL, traySettingsPath)
	case 3:
		if m.cancel != nil {
			m.cancel()
		}
	}
	return nil
}

func (m *linuxMenu) AboutToShow(id int32) (bool, *dbus.Error) {
	_ = id
	return false, nil
}

// runWindowsTray on Linux registers a StatusNotifierItem when a session bus is available.
func runWindowsTray(ctx context.Context, cancel context.CancelFunc, uiURL string) bool {
	if !hasDisplay() {
		return false
	}
	go func() {
		waitHealth(uiURL)
		openUI(uiURL)
	}()
	if err := runLinuxTray(ctx, cancel, uiURL); err != nil {
		<-ctx.Done()
		return true
	}
	return true
}

func runLinuxTray(ctx context.Context, cancel context.CancelFunc, uiURL string) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()

	openLabel, settingsLabel, quitLabel := trayMenuLabels(trayChineseLocale())
	sni := &linuxSNI{uiURL: uiURL, cancel: cancel}
	menu := &linuxMenu{uiURL: uiURL, cancel: cancel, open: openLabel, settings: settingsLabel, quit: quitLabel}

	if err := conn.Export(sni, sniPath, sniIface); err != nil {
		return err
	}
	if err := conn.Export(menu, menuPath, menuIface); err != nil {
		return err
	}

	pixmaps := meshTrayIconPixmaps()
	sniProps := map[string]map[string]*prop.Prop{
		sniIface: {
			"Category":    {Value: "ApplicationStatus", Writable: false, Emit: prop.EmitTrue},
			"Id":          {Value: "agent-mesh-node", Writable: false, Emit: prop.EmitTrue},
			"Title":       {Value: "Agent Mesh", Writable: false, Emit: prop.EmitTrue},
			"Status":      {Value: "Active", Writable: false, Emit: prop.EmitTrue},
			"IconName":    {Value: "", Writable: false, Emit: prop.EmitTrue},
			"IconPixmap":  {Value: pixmaps, Writable: false, Emit: prop.EmitTrue},
			"ItemIsMenu":  {Value: false, Writable: false, Emit: prop.EmitTrue},
			"Menu":        {Value: dbus.ObjectPath(menuPath), Writable: false, Emit: prop.EmitTrue},
		},
	}
	if _, err := prop.Export(conn, sniPath, sniProps); err != nil {
		return err
	}
	menuProps := map[string]map[string]*prop.Prop{
		menuIface: {
			"Version":        {Value: uint32(3), Writable: false, Emit: prop.EmitTrue},
			"TextDirection":  {Value: "ltr", Writable: false, Emit: prop.EmitTrue},
			"Status":         {Value: "normal", Writable: false, Emit: prop.EmitTrue},
			"IconThemePath":  {Value: []string{}, Writable: false, Emit: prop.EmitTrue},
		},
	}
	if _, err := prop.Export(conn, menuPath, menuProps); err != nil {
		return err
	}
	if err := conn.Export(introspect.Introspectable(sniIntrospect), sniPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	if err := conn.Export(introspect.Introspectable(menuIntrospect), menuPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}

	name := fmt.Sprintf("org.agentmesh.NodeStatusNotifier%d", os.Getpid())
	reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("tray bus name busy")
	}

	watcher := conn.Object(watcherName, watcherPath)
	if call := watcher.Call(watcherName+".RegisterStatusNotifierItem", 0, sniPath); call.Err != nil {
		return call.Err
	}

	<-ctx.Done()
	return nil
}
