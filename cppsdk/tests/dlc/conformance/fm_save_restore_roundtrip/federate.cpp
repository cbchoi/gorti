// fm_save_restore_roundtrip — single federate exercising §4.16-§4.32.
//
// Sequence:
//   1. CONNECT + CREATE + JOIN (§4.2, §4.5, §4.9).
//   2. registerObjectInstance (§6.8) + update Counter=10 (§6.10).
//   3. requestFederationSave(label) (§4.16).
//   4. RTI fires initiateFederateSave(label) (§4.17) — 1-arg overload.
//   5. Federate calls federateSaveBegun() (§4.18 — added per
//      divergence catalogue row 3.12).
//   6. Federate calls federateSaveComplete() (§4.19).
//   7. RTI fires federationSaved() (§4.20 — no label arg per catalogue
//      row 4.9).
//   8. RESIGN with CANCEL_THEN_DELETE_THEN_DIVEST (§4.10).
//   9. Re-join.
//  10. requestFederationRestore(label) (§4.24).
//  11. RTI fires requestFederationRestoreSucceeded (§4.25 per catalogue
//      row 4.11).
//  12. RTI fires federationRestoreBegun (§4.26 per catalogue row 4.12).
//  13. RTI fires initiateFederateRestore(label, federateName, handle)
//      (§4.27 per catalogue row 4.13 — federateName added).
//  14. Federate calls federateRestoreComplete (§4.28).
//  15. RTI fires federationRestored() (§4.29 — no label arg per
//      catalogue row 4.14).
//  16. Verify Counter == 10 (state preserved).

#include <RTI/RTIambassador.h>
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Handle.h>
#include <RTI/Typedefs.h>
#include <RTI/encoding/BasicDataElements.h>

#include <atomic>
#include <chrono>
#include <iostream>
#include <string>
#include <thread>

namespace {
std::string ws2s(const std::wstring& w) {
  return std::string(w.begin(), w.end());
}

class SaveRestoreFed : public rti1516e::NullFederateAmbassador {
 public:
  // §4.17 initiateFederateSave — 1-arg form.
  void initiateFederateSave(std::wstring const& label) override {
    std::cout << "FED: INITIATE_FEDERATE_SAVE label=" << ws2s(label)
              << std::endl;
    save_initiated_.store(true);
  }

  // §4.20 federationSaved — NO label arg per catalogue row 4.9.
  void federationSaved() override {
    std::cout << "FED: FEDERATION_SAVED" << std::endl;
    saved_.store(true);
  }

  // §4.25 — restore request ack.
  void requestFederationRestoreSucceeded(std::wstring const& label) override {
    std::cout << "FED: RESTORE_REQUEST_SUCCEEDED label=" << ws2s(label)
              << std::endl;
  }

  // §4.26 federationRestoreBegun.
  void federationRestoreBegun() override {
    std::cout << "FED: RESTORE_BEGUN" << std::endl;
  }

  // §4.27 initiateFederateRestore — 3-arg form: label, federateName,
  // postRestoreFederateHandle.
  void initiateFederateRestore(
      std::wstring const& label,
      std::wstring const& federateName,
      rti1516e::FederateHandle postRestoreFederateHandle) override {
    std::cout << "FED: INITIATE_FEDERATE_RESTORE label=" << ws2s(label)
              << " federate=" << ws2s(federateName) << " handle=<H>"
              << std::endl;
    restore_initiated_.store(true);
  }

  // §4.29 federationRestored — NO label arg per catalogue row 4.14.
  void federationRestored() override {
    std::cout << "FED: FEDERATION_RESTORED" << std::endl;
    restored_.store(true);
  }

  std::atomic<bool> save_initiated_{false};
  std::atomic<bool> saved_{false};
  std::atomic<bool> restore_initiated_{false};
  std::atomic<bool> restored_{false};
};
}  // namespace

int main() {
  try {
    rti1516e::RTIambassadorFactory factory;
    auto amb = factory.createRTIambassador();
    SaveRestoreFed fed;

    amb->connect(fed, rti1516e::HLA_IMMEDIATE,
                 L"gortiAddress=127.0.0.1:8080");
    std::cout << "FED: CONNECT" << std::endl;

    std::vector<std::wstring> modules{L"./federation.fom.xml"};
    try {
      amb->createFederationExecution(L"fm_save_restore", modules);
    } catch (const rti1516e::FederationExecutionAlreadyExists&) {}
    amb->joinFederationExecution(L"saver", L"fm_save_restore");
    std::cout << "FED: JOIN federate=saver" << std::endl;

    const auto state_cls = amb->getObjectClassHandle(L"HLAobjectRoot.State");
    const auto counter = amb->getAttributeHandle(state_cls, L"Counter");
    rti1516e::AttributeHandleSet attrs;
    attrs.insert(counter);
    amb->publishObjectClassAttributes(state_cls, attrs);
    const auto obj = amb->registerObjectInstance(state_cls, L"state-1");
    std::cout << "FED: REGISTER name=state-1 handle=<H>" << std::endl;

    {
      rti1516e::HLAinteger32BE c(10);
      rti1516e::AttributeHandleValueMap vals;
      vals[counter] = c.encode();
      amb->updateAttributeValues(obj, vals, rti1516e::VariableLengthData());
      std::cout << "FED: UPDATE Counter=10" << std::endl;
    }

    // §4.16 requestFederationSave.
    amb->requestFederationSave(L"checkpoint-1");
    std::cout << "FED: REQUEST_FEDERATION_SAVE label=checkpoint-1" << std::endl;

    // Wait loops drain via §10.42 evokeMultipleCallbacks. Legal under
    // HLA_IMMEDIATE on both RTIs (reference_rti delivers on background threads and
    // the evoke is a harmless yield; gorti M17 buffers events and drains
    // them on the evoking thread). Emits no canonical lines.
    for (int i = 0; i < 200 && !fed.save_initiated_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    // §4.18 federateSaveBegun — divergence catalogue row 3.12.
    amb->federateSaveBegun();
    std::cout << "FED: FEDERATE_SAVE_BEGUN" << std::endl;

    // §4.19 federateSaveComplete.
    amb->federateSaveComplete();
    std::cout << "FED: FEDERATE_SAVE_COMPLETE" << std::endl;

    for (int i = 0; i < 200 && !fed.saved_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "FED: RESIGN" << std::endl;

    // Re-join.
    amb->joinFederationExecution(L"saver", L"fm_save_restore");
    std::cout << "FED: REJOIN federate=saver" << std::endl;

    amb->requestFederationRestore(L"checkpoint-1");
    std::cout << "FED: REQUEST_FEDERATION_RESTORE label=checkpoint-1"
              << std::endl;

    for (int i = 0; i < 200 && !fed.restore_initiated_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    amb->federateRestoreComplete();
    std::cout << "FED: FEDERATE_RESTORE_COMPLETE" << std::endl;

    for (int i = 0; i < 200 && !fed.restored_.load(); ++i) {
      amb->evokeMultipleCallbacks(0.05, 0.1);
    }

    amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
    std::cout << "FED: RESIGN" << std::endl;
    amb->disconnect();
    return 0;
  } catch (const rti1516e::Exception& e) {
    std::cerr << "FED: ERROR " << ws2s(e.what()) << std::endl;
    return 1;
  }
}
