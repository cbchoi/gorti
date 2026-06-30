// IEEE 1516.1-2010 §10.5 / Annex A — RTI/Handle.h
// gorti M31 forward-declaration stub. Spec text reprinted with permission
// from IEEE 1516.1(TM)-2010.
//
// Defines 9 typed handle classes via the DEFINE_HANDLE_CLASS macro per
// §10.5. Each handle is a distinct C++ class (type-safe — no implicit
// conversion between, e.g., AttributeHandle and ParameterHandle).
// Catalogue rows 7.1-7.7.

#ifndef RTI_Handle_h
#define RTI_Handle_h

#include <RTI/SpecificConfig.h>
#include <RTI/Exception.h>
#include <RTI/VariableLengthData.h>
#include <string>
#include <iosfwd>

#define DEFINE_HANDLE_CLASS(HandleKind)                                  \
                                                                         \
  class HandleKind##Implementation;                                      \
                                                                         \
  class RTI_EXPORT HandleKind {                                          \
   public:                                                               \
    HandleKind();                                                        \
    ~HandleKind() RTI_NOEXCEPT;                                          \
    HandleKind(HandleKind const& rhs);                                   \
    HandleKind& operator=(HandleKind const& rhs);                        \
                                                                         \
    bool isValid() const;                                                \
                                                                         \
    bool operator==(HandleKind const& rhs) const;                        \
    bool operator!=(HandleKind const& rhs) const;                        \
    bool operator<(HandleKind const& rhs) const;                         \
                                                                         \
    long hash() const;                                                   \
                                                                         \
    VariableLengthData encode() const;                                   \
    void encode(VariableLengthData& buffer) const;                       \
    size_t encode(void* buffer, size_t bufferSize) const                 \
        RTI_THROW(CouldNotEncode);                                       \
    size_t encodedLength() const;                                        \
                                                                         \
    std::wstring toString() const;                                       \
                                                                         \
   protected:                                                            \
    friend class HandleKind##Friend;                                     \
    const HandleKind##Implementation* getImplementation() const;         \
    HandleKind##Implementation* getImplementation();                     \
    explicit HandleKind(HandleKind##Implementation* impl);               \
    explicit HandleKind(VariableLengthData const& encodedValue);         \
                                                                         \
    HandleKind##Implementation* _impl;                                   \
  };                                                                     \
                                                                         \
  std::wostream RTI_EXPORT& operator<<(std::wostream&, HandleKind const&);

namespace rti1516e {

// §10.5 — all 9 typed handle classes. The Pitch-listed names are the spec
// names; gorti drops `RoutingSpaceHandle` (HLA Evolved removed routing
// spaces; catalogue row 10.8 / FR-DLC-15).
DEFINE_HANDLE_CLASS(FederateHandle)
DEFINE_HANDLE_CLASS(ObjectClassHandle)
DEFINE_HANDLE_CLASS(InteractionClassHandle)
DEFINE_HANDLE_CLASS(ObjectInstanceHandle)
DEFINE_HANDLE_CLASS(AttributeHandle)
DEFINE_HANDLE_CLASS(ParameterHandle)
DEFINE_HANDLE_CLASS(DimensionHandle)
DEFINE_HANDLE_CLASS(MessageRetractionHandle)
DEFINE_HANDLE_CLASS(RegionHandle)

}  // namespace rti1516e

#endif  // RTI_Handle_h
