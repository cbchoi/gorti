// Lockfile: RTIambassador::connect signature per IEEE 1516.1-2010 §4.2.
// Fails to compile until RTI/RTIambassador.h exports the spec signature.
//
// Catalogue rows covered: 3.1 (connect signature), 3.2 (CallbackModel enum),
//                         3.3 (disconnect signature), 5.1 (CallbackModel enum
//                         vs gorti absent), FR-DLC-3, FR-DLC-11, FR-DLC-16.

#include <RTI/RTIambassador.h>
#include <RTI/FederateAmbassador.h>
#include <RTI/Enums.h>
#include <RTI/Exception.h>
#include <type_traits>
#include <string>

namespace {

using rti1516e::CallbackModel;
using rti1516e::FederateAmbassador;
using rti1516e::RTIambassador;
using rti1516e::HLA_IMMEDIATE;        // CRITICAL: unscoped enumerator per FR-DLC-16
using rti1516e::HLA_EVOKED;

// §4.2 — connect(FederateAmbassador&, CallbackModel, wstring const& localSettings)
// NOTE: bare HLA_IMMEDIATE, NOT CallbackModel::HLA_IMMEDIATE. The spec defines
// CallbackModel as an unscoped enum (reference_rti Enums.h:21-25); scoped-form access
// would require `enum class`, which breaks source-compat with reference_rti federates.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().connect(
        std::declval<FederateAmbassador&>(),
        HLA_IMMEDIATE,
        std::declval<std::wstring const&>())),
    void>);

// FR-DLC-16 lockfile — CallbackModel is UNSCOPED.
// `decltype(HLA_IMMEDIATE)` must be exactly the enum type — proving the
// enumerator is reachable WITHOUT the `CallbackModel::` qualifier.
static_assert(std::is_same_v<decltype(HLA_IMMEDIATE), CallbackModel>);
static_assert(std::is_same_v<decltype(HLA_EVOKED), CallbackModel>);

// FR-DLC-16 — enumerator values match spec (reference_rti Enums.h:21-25).
//             HLA_IMMEDIATE = 0, HLA_EVOKED = 1.
static_assert(static_cast<int>(HLA_IMMEDIATE) == 0);
static_assert(static_cast<int>(HLA_EVOKED) == 1);

// FR-DLC-16 — `CallbackModel` must NOT be an `enum class` (scoped enums
//              are not implicitly convertible to int). The cast above already
//              proves implicit-int convertibility; this is the explicit lock.
static_assert(std::is_convertible_v<CallbackModel, int>);

// §4.2 — the 2-arg overload (no localSettings — reference_rtiambassador.h:45-55
//        declares localSettings with default `L""`).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().connect(
        std::declval<FederateAmbassador&>(),
        HLA_IMMEDIATE)),
    void>);

// §4.2 — gorti M17 surface accepted `connect(string const& url)`. The DLC
//        surface does NOT. Lock that connect with a single std::string arg
//        does not match the spec signature; only the 2/3-arg form does.
//        (Cannot use static_assert(!exists), but we can lock the spec-correct
//        form via decltype.)

// §4.3 — disconnect() takes no args and returns void (catalogue 3.3 — drop the
//        non-spec isConnected() helper).
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().disconnect()),
    void>);

// §4.2 — connect declares throws of ConnectionFailed, InvalidLocalSettingsDesignator,
//        UnsupportedCallbackModel, AlreadyConnected, CallNotAllowedFromWithinCallback,
//        RTIinternalError. We at least lock the existence + Exception-derived nature
//        of the spec-mandated ConnectionFailed.
static_assert(std::is_class_v<rti1516e::ConnectionFailed>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::ConnectionFailed>);
static_assert(std::is_class_v<rti1516e::UnsupportedCallbackModel>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::UnsupportedCallbackModel>);
static_assert(std::is_class_v<rti1516e::AlreadyConnected>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::AlreadyConnected>);
static_assert(std::is_class_v<rti1516e::CallNotAllowedFromWithinCallback>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::CallNotAllowedFromWithinCallback>);
static_assert(std::is_class_v<rti1516e::InvalidLocalSettingsDesignator>);

}  // namespace
