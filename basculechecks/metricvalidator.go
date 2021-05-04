/**
 * Copyright 2021 Comcast Cable Communications Management, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package basculechecks

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cast"
	"github.com/xmidt-org/bascule"
	"go.uber.org/fx"
)

var (
	ErrGettingPartnerIDs = errWithReason{
		err:    errors.New("couldn't get partner IDs from attributes"),
		reason: UndeterminedPartnerID,
	}
	ErrPartnerIDsNotStringSlice = errWithReason{
		err:    errors.New("expected a string slice"),
		reason: UndeterminedPartnerID,
	}
)

// CapabilitiesChecker is an object that can determine if a request is
// authorized given a bascule.Authentication object.  If it's not authorized, an
//  error is given for logging and metrics.
type CapabilitiesChecker interface {
	CheckAuthentication(auth bascule.Authentication, vals ParsedValues) error
}

// Reasoner is an error that provides a failure reason to use as a value for a
// metric label.
type Reasoner interface {
	Reason() string
}

// ParsedValues are values determined from the bascule Authentication.
type ParsedValues struct {
	// Endpoint is the string representation of a regular expression that
	// matches the URL for the request.  The main benefit of this string is it
	// most likely won't include strings that change from one request to the
	// next (ie, device ID).
	Endpoint string
	// Partner is a string representation of the list of partners found in the
	// JWT, where:
	//   - any list including "*" as a partner is determined to be "wildcard".
	//   - when the list is <1 item, the partner is determined to be "none".
	//   - when the list is >1 item, the partner is determined to be "many".
	//   - when the list is only one item, that is the partner value.
	Partner string
}

// MetricValidator determines if a request is authorized and then updates a
// metric to show those results.
type MetricValidator struct {
	C         CapabilitiesChecker
	Measures  *AuthCapabilityCheckMeasures
	Endpoints []*regexp.Regexp
	ErrorOut  bool
	Server    string
}

// Check is a function for authorization middleware.  The function parses the
// information needed for the CapabilitiesChecker, calls it to determine if the
// request is authorized, and maintains the results in a metric.  The function
// can mark the request as unauthorized or only update the metric and allow the
// request, depending on configuration.  This allows for monitoring before being
// more strict with authorization.
func (m MetricValidator) Check(ctx context.Context, _ bascule.Token) error {
	// if we're not supposed to error out, the outcome should be accepted on failure
	failureOutcome := AcceptedOutcome
	if m.ErrorOut {
		// if we actually error out, the outcome is the request being rejected
		failureOutcome = RejectedOutcome
	}

	auth, ok := bascule.FromContext(ctx)
	if !ok {
		m.Measures.CapabilityCheckOutcome.With(prometheus.Labels{
			ServerLabel:    m.Server,
			OutcomeLabel:   failureOutcome,
			ReasonLabel:    TokenMissing,
			ClientIDLabel:  "",
			PartnerIDLabel: "",
			EndpointLabel:  "",
			MethodLabel:    "",
		}).Add(1)
		if m.ErrorOut {
			return ErrNoAuth
		}
		return nil
	}

	client, partnerID, endpoint, err := m.prepMetrics(auth)
	labels := prometheus.Labels{
		ServerLabel:    m.Server,
		ClientIDLabel:  client,
		PartnerIDLabel: partnerID,
		EndpointLabel:  endpoint,
		MethodLabel:    auth.Request.Method,
		OutcomeLabel:   AcceptedOutcome,
		ReasonLabel:    "",
	}
	if err != nil {
		labels[OutcomeLabel] = failureOutcome
		labels[ReasonLabel] = UnknownReason
		var r Reasoner
		if errors.As(err, &r) {
			labels[ReasonLabel] = r.Reason()
		}
		m.Measures.CapabilityCheckOutcome.With(labels).Add(1)
		if m.ErrorOut {
			return err
		}
		return nil
	}

	v := ParsedValues{
		Endpoint: endpoint,
		Partner:  partnerID,
	}

	err = m.C.CheckAuthentication(auth, v)
	if err != nil {
		labels[OutcomeLabel] = failureOutcome
		labels[ReasonLabel] = UnknownReason
		var r Reasoner
		if errors.As(err, &r) {
			labels[ReasonLabel] = r.Reason()
		}
		m.Measures.CapabilityCheckOutcome.With(labels).Add(1)
		if m.ErrorOut {
			return fmt.Errorf("endpoint auth for %v on %v failed: %v",
				auth.Request.Method, auth.Request.URL.EscapedPath(), err)
		}
		return nil
	}

	m.Measures.CapabilityCheckOutcome.With(labels).Add(1)
	return nil
}

// prepMetrics gathers the information needed for metric label information.  It
// gathers the client ID, partnerID, and endpoint (bucketed) for more information
// on the metric when a request is unauthorized.
func (m MetricValidator) prepMetrics(auth bascule.Authentication) (string, string, string, error) {
	if auth.Token == nil {
		return "", "", "", ErrNoToken
	}
	if len(auth.Request.Method) == 0 {
		return "", "", "", ErrNoMethod
	}
	client := auth.Token.Principal()
	if auth.Token.Attributes() == nil {
		return client, "", "", ErrNilAttributes
	}

	partnerVal, ok := bascule.GetNestedAttribute(auth.Token.Attributes(), PartnerKeys()...)
	if !ok {
		err := fmt.Errorf("%w using keys %v", ErrGettingPartnerIDs, PartnerKeys())
		return client, "", "", err
	}
	partnerIDs, err := cast.ToStringSliceE(partnerVal)
	if err != nil {
		err = fmt.Errorf("%w for partner IDs \"%v\": %v",
			ErrPartnerIDsNotStringSlice, partnerVal, err)
		return client, "", "", err
	}
	partnerID := DeterminePartnerMetric(partnerIDs)

	if auth.Request.URL == nil {
		return client, partnerID, "", ErrNoURL
	}
	escapedURL := auth.Request.URL.EscapedPath()
	endpoint := determineEndpointMetric(m.Endpoints, escapedURL)
	return client, partnerID, endpoint, nil
}

// DeterminePartnerMetric takes a list of partners and decides what the partner
// metric label should be.
func DeterminePartnerMetric(partners []string) string {
	if len(partners) < 1 {
		return "none"
	}
	if len(partners) == 1 {
		if partners[0] == "*" {
			return "wildcard"
		}
		return partners[0]
	}
	for _, partner := range partners {
		if partner == "*" {
			return "wildcard"
		}
	}
	return "many"
}

// determineEndpointMetric takes a list of regular expressions and applies them
// to the url of the request to decide what the endpoint metric label should be.
func determineEndpointMetric(endpoints []*regexp.Regexp, urlHit string) string {
	for _, r := range endpoints {
		idxs := r.FindStringIndex(urlHit)
		if idxs == nil {
			continue
		}
		if idxs[0] == 0 {
			return r.String()
		}
	}
	return "not_recognized"
}

func ProvideMetricValidator(server string) fx.Option {
	return fx.Provide(
		fx.Annotated{
			Name: fmt.Sprintf("%s_bascule_capability_measures", server),
			// TODO: this will be fixed when Metric Validator gets its own New()
			// function and Options.
			Target: func(checker CapabilitiesChecker, measures *AuthCapabilityCheckMeasures,
				endpoints []*regexp.Regexp, errorOut bool) MetricValidator {
				return MetricValidator{
					C:         checker,
					Measures:  measures,
					Endpoints: endpoints,
					ErrorOut:  errorOut,
					Server:    server,
				}
			},
		},
	)
}
