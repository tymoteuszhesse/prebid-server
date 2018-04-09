package openx

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mxmCherry/openrtb"
	"github.com/prebid/prebid-server/adapters"
	"github.com/prebid/prebid-server/openrtb_ext"
)

const uri = "http://rtb.openx.net/prebid"
const config = "hb_pbs_1.0.0"

type OpenxAdapter struct {
}

type openxImpExt struct {
	CustomParams map[string]interface{} `json:"customParams,omitempty"`
}

type openxReqExt struct {
	DelDomain    string `json:"delDomain"`
	BidderConfig string `json:"bc"`
}

func (a *OpenxAdapter) MakeRequests(request *openrtb.BidRequest) ([]*adapters.RequestData, []error) {
	var errs []error
	var bannerImps []openrtb.Imp
	var videoImps []openrtb.Imp

	for _, imp := range request.Imp {
		// OpenX doesn't allow multi-type imp. Banner takes priority over video.
		if imp.Banner != nil {
			bannerImps = append(bannerImps, imp)
		} else if imp.Video != nil {
			videoImps = append(videoImps, imp)
		} else {
			err := fmt.Errorf("OpenX only supports banner and video imps. Ignoring imp id=%s", imp.ID)
			errs = append(errs, err)
		}
	}

	var adapterRequests []*adapters.RequestData
	// Make a copy as we don't want to change the original request
	reqCopy := *request

	reqCopy.Imp = bannerImps
	adapterReq, errors := makeRequest(&reqCopy)
	if adapterReq != nil {
		adapterRequests = append(adapterRequests, adapterReq)
	}
	errs = append(errs, errors...)

	// OpenX only supports single imp video request
	for _, videoImp := range videoImps {
		reqCopy.Imp = []openrtb.Imp{videoImp}
		adapterReq, errors := makeRequest(&reqCopy)
		if adapterReq != nil {
			adapterRequests = append(adapterRequests, adapterReq)
		}
		errs = append(errs, errors...)
	}

	return adapterRequests, errs
}

func makeRequest(request *openrtb.BidRequest) (*adapters.RequestData, []error) {
	var errs []error
	var validImps []openrtb.Imp
	reqExt := openxReqExt{BidderConfig: config}

	for _, imp := range request.Imp {
		if err := preprocess(&imp, &reqExt); err != nil {
			errs = append(errs, err)
			continue
		}
		validImps = append(validImps, imp)
	}

	// If all the imps were malformed, don't bother making a server call with no impressions.
	if len(validImps) == 0 {
		return nil, errs
	}

	request.Imp = validImps

	var err error
	request.Ext, err = json.Marshal(reqExt)
	if err != nil {
		errs = append(errs, err)
		return nil, errs
	}

	reqJSON, err := json.Marshal(request)
	if err != nil {
		errs = append(errs, err)
		return nil, errs
	}

	headers := http.Header{}
	headers.Add("Content-Type", "application/json;charset=utf-8")
	headers.Add("Accept", "application/json")
	return &adapters.RequestData{
		Method:  "POST",
		Uri:     uri,
		Body:    reqJSON,
		Headers: headers,
	}, errs
}

// Mutate the imp to get it ready to send to openx.
func preprocess(imp *openrtb.Imp, reqExt *openxReqExt) error {
	var bidderExt adapters.ExtImpBidder
	if err := json.Unmarshal(imp.Ext, &bidderExt); err != nil {
		return err
	}

	var openxExt openrtb_ext.ExtImpOpenx
	if err := json.Unmarshal(bidderExt.Bidder, &openxExt); err != nil {
		return err
	}

	reqExt.DelDomain = openxExt.DelDomain

	imp.TagID = openxExt.Unit
	imp.BidFloor = openxExt.CustomFloor
	imp.Ext = nil

	if openxExt.CustomParams != nil {
		impExt := openxImpExt{
			CustomParams: openxExt.CustomParams,
		}
		var err error
		if imp.Ext, err = json.Marshal(impExt); err != nil {
			return err
		}
	}

	return nil
}

func (a *OpenxAdapter) MakeBids(internalRequest *openrtb.BidRequest, externalRequest *adapters.RequestData, response *adapters.ResponseData) ([]*adapters.TypedBid, []error) {
	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if response.StatusCode != http.StatusOK {
		return nil, []error{fmt.Errorf("Unexpected status code: %d. Run with request.debug = 1 for more info", response.StatusCode)}
	}

	var bidResp openrtb.BidResponse
	if err := json.Unmarshal(response.Body, &bidResp); err != nil {
		return nil, []error{err}
	}

	bids := make([]*adapters.TypedBid, 0, 5)

	for _, sb := range bidResp.SeatBid {
		for i := range sb.Bid {
			bids = append(bids, &adapters.TypedBid{
				Bid:     &sb.Bid[i],
				BidType: getMediaTypeForImp(sb.Bid[i].ImpID, internalRequest.Imp),
			})
		}
	}
	return bids, nil
}

// getMediaTypeForImp figures out which media type this bid is for.
//
// OpenX doesn't support multi-type impressions.
// If both banner and video exist, take banner as we do not want in-banner video.
func getMediaTypeForImp(impId string, imps []openrtb.Imp) openrtb_ext.BidType {
	mediaType := openrtb_ext.BidTypeBanner
	for _, imp := range imps {
		if imp.ID == impId {
			if imp.Banner == nil && imp.Video != nil {
				mediaType = openrtb_ext.BidTypeVideo
			}
			return mediaType
		}
	}
	return mediaType
}

func NewOpenxBidder() *OpenxAdapter {
	return &OpenxAdapter{}
}
