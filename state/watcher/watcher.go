// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// The watcher package provides an interface for observing changes
// to arbitrary MongoDB documents that are maintained via the
// mgo/txn transaction package.
package watcher

import (
	"fmt"
	"strings"
	"time"

	"github.com/juju/errors"
	"github.com/juju/loggo"
	"gopkg.in/juju/worker.v1"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/tomb.v2"

	"github.com/juju/juju/mongo"
	jworker "github.com/juju/juju/worker"
)

// BaseWatcher represents watch methods on the worker
// responsible for watching for database changes.
type BaseWatcher interface {
	worker.Worker

	Dead() <-chan struct{}
	Err() error

	// Watch will send events on the Change channel whenever the document you
	// are watching is changed. Note that in order to not miss any changes, you
	// should start Watching the document before you read the document.
	// At this low level Watch layer, there will not be an initial event.
	// Instead, Watch is synchronous, the Watch will not return until the
	// watcher is registered.
	// TODO(jam): 2019-01-31 Update Watch() to return an error rather now
	// that it is synchronous
	Watch(collection string, id interface{}, ch chan<- Change)

	// WatchMulti is similar to Watch, it just allows you to watch a set of
	// documents in the same collection in one request. Just like Watch,
	// no event will be sent for documents that don't change.
	WatchMulti(collection string, ids []interface{}, ch chan<- Change) error

	// WatchCollection will give an event if any documents are modified/added/removed
	// from the collection.
	// TODO(jam): 2019-01-31 Update WatchCollection() to return an error rather now
	// that it is synchronous
	WatchCollection(collection string, ch chan<- Change)

	// WatchCollectionWithFilter will give an event if any documents are modified/added/removed
	// from the collection. Filter can be supplied to check if a given document
	// should send an event.
	// TODO(jam): 2019-01-31 Update WatchCollectionWithFilter() to return an error rather now
	// that it is synchronous
	WatchCollectionWithFilter(collection string, ch chan<- Change, filter func(interface{}) bool)

	// Unwatch is an asynchronous request to stop watching a given watch.
	// It is an error to try to Unwatch something that is not being watched.
	// Note that Unwatch can be called for things that have been registered with
	// either Watch() or WatchMulti(). For WatchCollection or WatchCollectionWithFilter
	// use UnwatchCollection.
	// TODO(jam): 2019-01-31 Currently Unwatching something that isn't watched
	// is a panic, should we make the method synchronous and turn it into an error?
	// Or just turn it into a no-op
	Unwatch(collection string, id interface{}, ch chan<- Change)

	// UnwatchCollection is used when you are done with a watch started with
	// either WatchCollection or WatchCollectionWithFilter. You must pass in
	// the same Change channel. Unwatching a collection that isn't being watched
	// is an error that will panic().
	UnwatchCollection(collection string, ch chan<- Change)
}

var logger = loggo.GetLogger("juju.state.watcher")

// A Watcher can watch any number of collections and documents for changes.
type Watcher struct {
	tomb         tomb.Tomb
	iteratorFunc func() mongo.Iterator
	log          *mgo.Collection

	// watches holds the observers managed by Watch/Unwatch.
	watches map[watchKey][]watchInfo

	// needSync is set when a synchronization should take
	// place.
	needSync bool

	// syncEvents and requestEvents contain the events to be
	// dispatched to the watcher channels. They're queued during
	// processing and flushed at the end to simplify the algorithm.
	// The two queues are separated because events from sync are
	// handled in reverse order due to the way the algorithm works.
	syncEvents, requestEvents []event

	// request is used to deliver requests from the public API into
	// the the goroutine loop.
	request chan interface{}

	// lastId is the most recent transaction id observed by a sync.
	lastId interface{}
}

// A Change holds information about a document change.
type Change struct {
	// C and Id hold the collection name and document _id field value.
	C  string
	Id interface{}

	// Revno is the latest known value for the document's txn-revno
	// field, or -1 if the document was deleted.
	Revno int64
}

type watchKey struct {
	c  string
	id interface{} // nil when watching collection
}

func (k watchKey) String() string {
	coll := fmt.Sprintf("collection %q", k.c)
	if k.id == nil {
		return coll
	}
	if s, ok := k.id.(string); ok {
		return fmt.Sprintf("document %q in %s", s, coll)
	}
	return fmt.Sprintf("document %v in %s", k.id, coll)
}

// match returns whether the receiving watch key,
// which may refer to a particular item or
// an entire collection, matches k1, which refers
// to a particular item.
func (k watchKey) match(k1 watchKey) bool {
	if k.c != k1.c {
		return false
	}
	if k.id == nil {
		// k refers to entire collection
		return true
	}
	return k.id == k1.id
}

type watchInfo struct {
	ch     chan<- Change
	revno  int64
	filter func(interface{}) bool
}

type event struct {
	ch    chan<- Change
	key   watchKey
	revno int64
}

// Period is the delay between each sync.
// It must not be changed when any watchers are active.
var Period time.Duration = 5 * time.Second

// New returns a new Watcher observing the changelog collection,
// which must be a capped collection maintained by mgo/txn.
func New(changelog *mgo.Collection) *Watcher {
	return newWatcher(changelog, nil)
}

func newWatcher(changelog *mgo.Collection, iteratorFunc func() mongo.Iterator) *Watcher {
	w := &Watcher{
		log:          changelog,
		iteratorFunc: iteratorFunc,
		watches:      make(map[watchKey][]watchInfo),
		request:      make(chan interface{}),
	}
	if w.iteratorFunc == nil {
		w.iteratorFunc = w.iter
	}
	w.tomb.Go(func() error {
		err := w.loop(Period)
		cause := errors.Cause(err)
		// tomb expects ErrDying or ErrStillAlive as
		// exact values, so we need to log and unwrap
		// the error first.
		if err != nil && cause != tomb.ErrDying {
			logger.Infof("watcher loop failed: %v", err)
		}
		return cause
	})
	return w
}

// NewDead returns a new watcher that is already dead
// and always returns the given error from its Err method.
func NewDead(err error) *Watcher {
	var w Watcher
	w.tomb.Kill(errors.Trace(err))
	return &w
}

// Kill is part of the worker.Worker interface.
func (w *Watcher) Kill() {
	w.tomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (w *Watcher) Wait() error {
	return w.tomb.Wait()
}

// Stop stops all the watcher activities.
func (w *Watcher) Stop() error {
	return worker.Stop(w)
}

// Dead returns a channel that is closed when the watcher has stopped.
func (w *Watcher) Dead() <-chan struct{} {
	return w.tomb.Dead()
}

// Err returns the error with which the watcher stopped.
// It returns nil if the watcher stopped cleanly, tomb.ErrStillAlive
// if the watcher is still running properly, or the respective error
// if the watcher is terminating or has terminated with an error.
func (w *Watcher) Err() error {
	return w.tomb.Err()
}

type reqWatch struct {
	key  watchKey
	info watchInfo
	// registeredCh is used to indicate when
	registeredCh chan error
}

func (r reqWatch) Completed() chan error {
	return r.registeredCh
}

type reqWatchMulti struct {
	collection  string
	ids         []interface{}
	completedCh chan error
	watchCh     chan<- Change
}

func (r reqWatchMulti) Completed() chan error {
	return r.completedCh
}

type reqUnwatch struct {
	key watchKey
	ch  chan<- Change
}

type reqSync struct{}

// waitableRequest represents a request that is made, and you wait for the core loop to acknowledge the request has been
// received
type waitableRequest interface {
	// Completed returns the channel that the core loop will use to signal completion of the request.
	Completed() chan error
}

func (w *Watcher) sendReq(req interface{}) {
	select {
	case w.request <- req:
	case <-w.tomb.Dying():
	}
}

func (w *Watcher) sendAndWaitReq(req waitableRequest) error {
	select {
	case w.request <- req:
	case <-w.tomb.Dying():
		return errors.Trace(tomb.ErrDying)
	}
	completed := req.Completed()
	select {
	case err := <-completed:
		return errors.Trace(err)
	case <-w.tomb.Dying():
		return errors.Trace(tomb.ErrDying)
	}
}

// Watchstarts watching the given collection and document id.
// An event will be sent onto ch whenever a matching document's txn-revno
// field is observed to change after a transaction is applied.
func (w *Watcher) Watch(collection string, id interface{}, ch chan<- Change) {
	if id == nil {
		panic("watcher: cannot watch a document with nil id")
	}
	w.sendAndWaitReq(reqWatch{
		key:          watchKey{collection, id},
		info:         watchInfo{ch, -2, nil},
		registeredCh: make(chan error),
	})
}

func (w *Watcher) WatchMulti(collection string, ids []interface{}, ch chan<- Change) error {
	for _, id := range ids {
		if id == nil {
			return errors.Errorf("cannot watch a document with nil id")
		}
	}
	req := reqWatchMulti{
		collection:  collection,
		ids:         ids,
		watchCh:     ch,
		completedCh: make(chan error),
	}
	w.sendReq(req)
	select {
	case err := <-req.completedCh:
		return errors.Trace(err)
	case <-w.tomb.Dying():
		return errors.Trace(tomb.ErrDying)
	}
}

// WatchCollection starts watching the given collection.
// An event will be sent onto ch whenever the txn-revno field is observed
// to change after a transaction is applied for any document in the collection.
func (w *Watcher) WatchCollection(collection string, ch chan<- Change) {
	w.WatchCollectionWithFilter(collection, ch, nil)
}

// WatchCollectionWithFilter starts watching the given collection.
// An event will be sent onto ch whenever the txn-revno field is observed
// to change after a transaction is applied for any document in the collection, so long as the
// specified filter function returns true when called with the document id value.
func (w *Watcher) WatchCollectionWithFilter(collection string, ch chan<- Change, filter func(interface{}) bool) {
	w.sendAndWaitReq(reqWatch{
		key:          watchKey{collection, nil},
		info:         watchInfo{ch, 0, filter},
		registeredCh: make(chan error),
	})
}

// Unwatch stops watching the given collection and document id via ch.
func (w *Watcher) Unwatch(collection string, id interface{}, ch chan<- Change) {
	if id == nil {
		panic("watcher: cannot unwatch a document with nil id")
	}
	w.sendReq(reqUnwatch{watchKey{collection, id}, ch})
}

// UnwatchCollection stops watching the given collection via ch.
func (w *Watcher) UnwatchCollection(collection string, ch chan<- Change) {
	w.sendReq(reqUnwatch{watchKey{collection, nil}, ch})
}

// StartSync forces the watcher to load new events from the database.
func (w *Watcher) StartSync() {
	w.sendReq(reqSync{})
}

// loop implements the main watcher loop.
// period is the delay between each sync.
func (w *Watcher) loop(period time.Duration) error {
	next := time.After(period)
	w.needSync = true
	if err := w.initLastId(); err != nil {
		return errors.Trace(err)
	}
	for {
		if w.needSync {
			if err := w.sync(); err != nil {
				// If the txn log collection overflows from underneath us,
				// the easiest cause of action to recover is to cause the
				// agen tto restart.
				if errors.Cause(err) == cappedPositionLostError {
					// Ideally we'd not import the worker package but that's
					// where all the errors are defined.
					return jworker.ErrRestartAgent
				}
				return errors.Trace(err)
			}
			w.flush()
			next = time.After(period)
		}
		select {
		case <-w.tomb.Dying():
			return errors.Trace(tomb.ErrDying)
		case <-next:
			next = time.After(period)
			w.needSync = true
		case req := <-w.request:
			w.handle(req)
			w.flush()
		}
	}
}

// flush sends all pending events to their respective channels.
func (w *Watcher) flush() {
	// refreshEvents are stored newest first.
	for i := len(w.syncEvents) - 1; i >= 0; i-- {
		e := &w.syncEvents[i]
		for e.ch != nil {
			change := Change{
				C:     e.key.c,
				Id:    e.key.id,
				Revno: e.revno,
			}
			select {
			case <-w.tomb.Dying():
				return
			case req := <-w.request:
				w.handle(req)
				continue
			case e.ch <- change:
			}
			break
		}
	}
	// requestEvents are stored oldest first, and
	// may grow during the loop.
	for i := 0; i < len(w.requestEvents); i++ {
		e := &w.requestEvents[i]
		for e.ch != nil {
			change := Change{
				C:     e.key.c,
				Id:    e.key.id,
				Revno: e.revno,
			}
			select {
			case <-w.tomb.Dying():
				return
			case req := <-w.request:
				w.handle(req)
				continue
			case e.ch <- change:
			}
			break
		}
	}
	w.syncEvents = w.syncEvents[:0]
	w.requestEvents = w.requestEvents[:0]
}

// handle deals with requests delivered by the public API
// onto the background watcher goroutine.
func (w *Watcher) handle(req interface{}) {
	logger.Tracef("got request: %#v", req)
	switch r := req.(type) {
	case reqSync:
		w.needSync = true
	case reqWatch:
		for _, info := range w.watches[r.key] {
			if info.ch == r.info.ch {
				panic(fmt.Errorf("tried to re-add channel %v for %s", info.ch, r.key))
			}
		}
		w.watches[r.key] = append(w.watches[r.key], r.info)
		if r.registeredCh != nil {
			select {
			case r.registeredCh <- nil:
			case <-w.tomb.Dying():
			}
		}
	case reqUnwatch:
		watches := w.watches[r.key]
		removed := false
		for i, info := range watches {
			if info.ch == r.ch {
				watches[i] = watches[len(watches)-1]
				w.watches[r.key] = watches[:len(watches)-1]
				removed = true
				break
			}
		}
		if !removed {
			panic(fmt.Errorf("tried to remove missing channel %v for %s", r.ch, r.key))
		}
		for i := range w.requestEvents {
			e := &w.requestEvents[i]
			if r.key.match(e.key) && e.ch == r.ch {
				e.ch = nil
			}
		}
		for i := range w.syncEvents {
			e := &w.syncEvents[i]
			if r.key.match(e.key) && e.ch == r.ch {
				e.ch = nil
			}
		}
	case reqWatchMulti:
		for _, id := range r.ids {
			key := watchKey{c: r.collection, id: id}
			for _, info := range w.watches[key] {
				if info.ch == r.watchCh {
					err := errors.Errorf("tried to re-add channel %v for %s", info.ch, key)
					select {
					case r.completedCh <- err:
					case <-w.tomb.Dying():
					}
					return
				}
			}
		}
		for _, id := range r.ids {
			key := watchKey{c: r.collection, id: id}
			w.watches[key] = append(w.watches[key], watchInfo{ch: r.watchCh, revno: -2, filter: nil})
		}
		select {
		case r.completedCh <- nil:
		case <-w.tomb.Dying():
		}
	default:
		panic(fmt.Errorf("unknown request: %T", req))
	}
}

// initLastId reads the most recent changelog document and initializes
// lastId with it. This causes all history that precedes the creation
// of the watcher to be ignored.
func (w *Watcher) initLastId() error {
	var entry struct {
		Id interface{} `bson:"_id"`
	}
	err := w.log.Find(nil).Sort("-$natural").One(&entry)
	if err != nil && err != mgo.ErrNotFound {
		return errors.Trace(err)
	}
	w.lastId = entry.Id
	return nil
}

func (w *Watcher) iter() mongo.Iterator {
	return w.log.Find(nil).Batch(10).Sort("-$natural").Iter()
}

var cappedPositionLostError = errors.New("capped position lost")

// sync updates the watcher knowledge from the database, and
// queues events to observing channels.
func (w *Watcher) sync() error {
	w.needSync = false
	// Iterate through log events in reverse insertion order (newest first).
	iter := w.iteratorFunc()
	seen := make(map[watchKey]bool)
	first := true
	lastId := w.lastId
	var entry bson.D
	for iter.Next(&entry) {
		if len(entry) == 0 {
			logger.Tracef("got empty changelog document")
		}
		id := entry[0]
		if id.Name != "_id" {
			panic("watcher: _id field isn't first entry")
		}
		if first {
			w.lastId = id.Value
			first = false
		}
		if id.Value == lastId {
			break
		}
		logger.Tracef("got changelog document: %#v", entry)
		for _, c := range entry[1:] {
			// See txn's Runner.ChangeLog for the structure of log entries.
			var d, r []interface{}
			dr, _ := c.Value.(bson.D)
			for _, item := range dr {
				switch item.Name {
				case "d":
					d, _ = item.Value.([]interface{})
				case "r":
					r, _ = item.Value.([]interface{})
				}
			}
			if len(d) == 0 || len(d) != len(r) {
				logger.Warningf("changelog has invalid collection document: %#v", c)
				continue
			}
			for i := len(d) - 1; i >= 0; i-- {
				key := watchKey{c.Name, d[i]}
				if seen[key] {
					continue
				}
				seen[key] = true
				revno, ok := r[i].(int64)
				if !ok {
					logger.Warningf("changelog has revno with type %T: %#v", r[i], r[i])
					continue
				}
				if revno < 0 {
					revno = -1
				}
				// Queue notifications for per-collection watches.
				for _, info := range w.watches[watchKey{c.Name, nil}] {
					if info.filter != nil && !info.filter(d[i]) {
						continue
					}
					evt := event{
						ch:    info.ch,
						key:   key,
						revno: revno,
					}
					w.syncEvents = append(w.syncEvents, evt)
				}
				// Queue notifications for per-document watches.
				infos := w.watches[key]
				for i, info := range infos {
					if revno > info.revno || (revno < 0 && info.revno >= 0) {
						infos[i].revno = revno
						evt := event{
							ch:    info.ch,
							key:   key,
							revno: revno,
						}
						w.syncEvents = append(w.syncEvents, evt)
					}
				}
			}
		}
	}
	if err := iter.Close(); err != nil {
		if qerr, ok := err.(*mgo.QueryError); ok {
			// CappedPositionLost is code 136.
			// Just in case that changes for some reason, we'll also check the error message.
			if qerr.Code == 136 || strings.Contains(qerr.Message, "CappedPositionLost") {
				logger.Warningf("watcher iterator failed due to txn log collection overflow")
				err = cappedPositionLostError
			}
		}
		return errors.Annotate(err, "watcher iteration error")
	}
	return nil
}
