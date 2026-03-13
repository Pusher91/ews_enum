package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ews_enum/internal/ews"
)

const enumCharset = "abcdefghijklmnopqrstuvwxyz0123456789"

type enumWork struct {
	prefix string
	depth  int
}

type enumStatus struct {
	Prefix          string
	Found           int
	Requests        int64
	TopLevelStarted int64
	TotalTopLevel   int64
}

type enumReporter struct {
	OnStatus      func(enumStatus)
	OnContact     func(ews.Contact)
	OnPrefixError func(string, error)
}

type enumResult struct {
	Contacts []ews.Contact
	Requests int64
}

type enumFailure struct {
	Kind     ews.ErrorKind
	Requests int64
	Cause    error
}

func contactKey(contact ews.Contact) string {
	if contact.EmailAddress != "" {
		return "email:" + strings.ToLower(contact.EmailAddress)
	}

	parts := []string{
		strings.ToLower(contact.DisplayName),
		strings.ToLower(contact.GivenName),
		strings.ToLower(contact.Surname),
		strings.ToLower(contact.Title),
		strings.ToLower(contact.Department),
		strings.ToLower(contact.Office),
		strings.ToLower(contact.Company),
		strings.ToLower(contact.Phone),
	}
	return "contact:" + strings.Join(parts, "\x1f")
}

func (e *enumFailure) Error() string {
	switch e.Kind {
	case ews.ErrorKindResolveDenied:
		return fmt.Sprintf("credentials are valid, but this account cannot use ResolveNames (%d requests attempted)", e.Requests)
	case ews.ErrorKindAuth:
		return fmt.Sprintf("authentication error (%d requests attempted)", e.Requests)
	default:
		if e.Cause != nil {
			return fmt.Sprintf("enumeration failed after %d requests: %v", e.Requests, e.Cause)
		}
		return fmt.Sprintf("enumeration failed (%d requests attempted)", e.Requests)
	}
}

func runEnum(cfg appConfig, stdout, stderr io.Writer) int {
	client := ews.NewClient(ews.ClientOpts{
		NTLM: cfg.NTLM, TimeoutSec: cfg.Timeout, MaxConns: cfg.Conns, UseCookies: true,
	})
	var outputMu sync.Mutex

	clearProgress := func() {
		fmt.Fprintf(stderr, "\r%-100s\r", "")
	}

	reporter := enumReporter{
		OnStatus: func(status enumStatus) {
			outputMu.Lock()
			defer outputMu.Unlock()
			fmt.Fprintf(
				stderr,
				"\r[*] [%d/%d] Resolving prefix: %-6s (found: %d, requests: %d)",
				status.TopLevelStarted,
				status.TotalTopLevel,
				status.Prefix,
				status.Found,
				status.Requests,
			)
		},
		OnContact: func(contact ews.Contact) {
			outputMu.Lock()
			defer outputMu.Unlock()
			clearProgress()
			printContact(stderr, contact)
		},
		OnPrefixError: func(prefix string, err error) {
			outputMu.Lock()
			defer outputMu.Unlock()
			clearProgress()
			fmt.Fprintf(stderr, "[!] Error on prefix %q: %v\n", prefix, err)
		},
	}

	fmt.Fprintf(stderr, "[*] Starting GAL enumeration against %s\n", cfg.URL)
	fmt.Fprintf(stderr, "[*] Max prefix depth: %d, workers: %d, connections: %d\n", cfg.Depth, cfg.Workers, cfg.Conns)

	result, err := enumerateGAL(client, cfg, reporter)
	outputMu.Lock()
	clearProgress()
	outputMu.Unlock()
	if err != nil {
		fmt.Fprintf(stderr, "[!] Enumeration failed: %v\n", err)
		return 2
	}

	fmt.Fprintf(stderr, "[*] Enumeration complete: %d unique entries, %d requests\n", len(result.Contacts), result.Requests)

	out, closeOut, err := openOutput(stdout, cfg.OutFile)
	if err != nil {
		fmt.Fprintf(stderr, "[!] Cannot create output file: %v\n", err)
		return 1
	}
	defer closeOut()

	if err := writeContacts(out, cfg.Format, result.Contacts); err != nil {
		fmt.Fprintf(stderr, "[!] Writing output failed: %v\n", err)
		return 1
	}

	if cfg.OutFile != "" {
		fmt.Fprintf(stderr, "[*] Results written to %s\n", cfg.OutFile)
	}

	return 0
}

func enumerateGAL(client *http.Client, cfg appConfig, reporter enumReporter) (enumResult, error) {
	var mu sync.Mutex
	seen := make(map[string]ews.Contact)
	totalTopLevel := int64(len(enumCharset))
	workCh := make(chan enumWork)
	submitCh := make(chan enumWork)
	completeCh := make(chan struct{})
	seedDoneCh := make(chan struct{})
	abortCh := make(chan struct{})
	coordinatorDone := make(chan struct{})
	var abortOnce sync.Once
	var workerWg sync.WaitGroup
	var requestCount atomic.Int64
	var topLevelStarted atomic.Int64
	var authErrors atomic.Int64
	var resolveDeniedErrors atomic.Int64
	var totalErrors atomic.Int64
	var anySuccess atomic.Bool
	var abortKind atomic.Int32
	var failureMu sync.Mutex
	var firstFailure error
	var firstFailureKind ews.ErrorKind
	var firstGenericFailure error
	var firstGenericFailureKind ews.ErrorKind

	abortThreshold := int64(cfg.Workers)
	if abortThreshold > totalTopLevel {
		abortThreshold = totalTopLevel
	}
	if abortThreshold < 3 {
		abortThreshold = 3
	}
	if abortThreshold > totalTopLevel {
		abortThreshold = totalTopLevel
	}

	recordFailure := func(kind ews.ErrorKind, err error) {
		if err == nil {
			return
		}
		failureMu.Lock()
		if kind == ews.ErrorKindUnknown {
			kind = ews.ErrorKindOf(err)
		}
		if firstFailure == nil {
			firstFailure = err
			firstFailureKind = kind
		}
		if kind != ews.ErrorKindAuth && kind != ews.ErrorKindResolveDenied && firstGenericFailure == nil {
			firstGenericFailure = err
			firstGenericFailureKind = kind
		}
		failureMu.Unlock()
	}

	abort := func(kind ews.ErrorKind, cause error) {
		if kind == ews.ErrorKindUnknown {
			return
		}
		if abortKind.CompareAndSwap(int32(ews.ErrorKindUnknown), int32(kind)) {
			recordFailure(kind, cause)
		}
		abortOnce.Do(func() { close(abortCh) })
	}

	reportStatus := func(prefix string) {
		if reporter.OnStatus == nil {
			return
		}
		mu.Lock()
		found := len(seen)
		mu.Unlock()
		reporter.OnStatus(enumStatus{
			Prefix:          prefix,
			Found:           found,
			Requests:        requestCount.Load(),
			TopLevelStarted: topLevelStarted.Load(),
			TotalTopLevel:   totalTopLevel,
		})
	}

	classifyGenericFailure := func(kind ews.ErrorKind) ews.ErrorKind {
		switch kind {
		case ews.ErrorKindServer, ews.ErrorKindParse, ews.ErrorKindTransport:
			return kind
		default:
			return ews.ErrorKindServer
		}
	}

	handleWork := func(item enumWork) {
		select {
		case <-abortCh:
			return
		default:
		}

		if cfg.DelayMS > 0 {
			time.Sleep(time.Duration(cfg.DelayMS) * time.Millisecond)
			select {
			case <-abortCh:
				return
			default:
			}
		}

		requestCount.Add(1)
		reportStatus(item.prefix)

		contacts, truncated, err := ews.ResolveNames(client, cfg.URL, cfg.User, cfg.Pass, item.prefix)
		if err != nil {
			if reporter.OnPrefixError != nil {
				reporter.OnPrefixError(item.prefix, err)
			}

			kind := ews.ErrorKindOf(err)
			recordFailure(kind, err)

			total := totalErrors.Add(1)
			switch kind {
			case ews.ErrorKindAuth:
				authErrors.Add(1)
			case ews.ErrorKindResolveDenied:
				resolveDeniedErrors.Add(1)
			}

			if !anySuccess.Load() && total >= abortThreshold {
				switch {
				case authErrors.Load() == total:
					abort(ews.ErrorKindAuth, err)
				case resolveDeniedErrors.Load() == total:
					abort(ews.ErrorKindResolveDenied, err)
				default:
					abort(classifyGenericFailure(kind), err)
				}
			}
			return
		}

		anySuccess.Store(true)

		var newContacts []ews.Contact
		mu.Lock()
		for _, contact := range contacts {
			key := contactKey(contact)
			if _, exists := seen[key]; !exists {
				seen[key] = contact
				newContacts = append(newContacts, contact)
			}
		}
		mu.Unlock()

		if reporter.OnContact != nil {
			for _, contact := range newContacts {
				reporter.OnContact(contact)
			}
		}

		if truncated && item.depth < cfg.Depth {
			for _, ch := range enumCharset {
				child := enumWork{prefix: item.prefix + string(ch), depth: item.depth + 1}
				select {
				case <-abortCh:
					return
				case submitCh <- child:
				}
			}
		}
	}

	go func() {
		defer close(workCh)
		defer close(coordinatorDone)

		queue := make([]enumWork, 0, len(enumCharset))
		active := 0
		seedDone := false
		seedDoneNotify := seedDoneCh
		abortNotify := abortCh
		aborting := false

		for {
			if seedDone && active == 0 && (aborting || len(queue) == 0) {
				return
			}

			var sendCh chan enumWork
			var next enumWork
			if !aborting && len(queue) > 0 {
				sendCh = workCh
				next = queue[0]
			}

			select {
			case <-abortNotify:
				aborting = true
				queue = nil
				abortNotify = nil
			case item := <-submitCh:
				if !aborting {
					queue = append(queue, item)
				}
			case <-completeCh:
				if active > 0 {
					active--
				}
			case <-seedDoneNotify:
				seedDone = true
				seedDoneNotify = nil
			case sendCh <- next:
				queue = queue[1:]
				active++
			}
		}
	}()

	worker := func() {
		defer workerWg.Done()
		for item := range workCh {
			if item.depth == 1 {
				topLevelStarted.Add(1)
			}
			handleWork(item)
			completeCh <- struct{}{}
		}
	}

	workerWg.Add(cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		go worker()
	}

seedTopLevel:
	for _, ch := range enumCharset {
		select {
		case <-abortCh:
			break seedTopLevel
		case submitCh <- enumWork{prefix: string(ch), depth: 1}:
		}
	}
	close(seedDoneCh)
	workerWg.Wait()
	<-coordinatorDone

	results := make([]ews.Contact, 0, len(seen))
	for _, contact := range seen {
		results = append(results, contact)
	}

	failureMu.Lock()
	cause := firstFailure
	causeKind := firstFailureKind
	genericCause := firstGenericFailure
	genericCauseKind := firstGenericFailureKind
	failureMu.Unlock()

	if kind := ews.ErrorKind(abortKind.Load()); kind != ews.ErrorKindUnknown {
		return enumResult{Contacts: results, Requests: requestCount.Load()}, &enumFailure{
			Kind:     kind,
			Requests: requestCount.Load(),
			Cause:    cause,
		}
	}

	if totalErrors.Load() > 0 {
		total := totalErrors.Load()
		var kind ews.ErrorKind
		finalCause := cause
		if anySuccess.Load() {
			if genericCause != nil {
				finalCause = genericCause
				kind = classifyGenericFailure(genericCauseKind)
			} else {
				kind = ews.ErrorKindServer
			}
		} else {
			switch {
			case authErrors.Load() == total:
				kind = ews.ErrorKindAuth
			case resolveDeniedErrors.Load() == total:
				kind = ews.ErrorKindResolveDenied
			default:
				if genericCause != nil {
					finalCause = genericCause
					kind = classifyGenericFailure(genericCauseKind)
				} else {
					kind = classifyGenericFailure(causeKind)
				}
			}
		}
		return enumResult{Contacts: results, Requests: requestCount.Load()}, &enumFailure{
			Kind:     kind,
			Requests: requestCount.Load(),
			Cause:    finalCause,
		}
	}

	return enumResult{Contacts: results, Requests: requestCount.Load()}, nil
}
