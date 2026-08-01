// IEEE 1516.1-2010 Annex A — VariableLengthData implementation.
//
// gorti M32 implementation. Catalogue row 8.1 / FR-DLC-4.
//
// Three ownership modes per spec:
//   setData(const, size)           — copy (own a new buffer)
//   setDataPointer(non-const, size)— borrow (do not delete)
//   takeDataPointer(non-const, size, deleter) — take (delete on dtor)
//
// Default construction yields an empty payload (data()==nullptr, size()==0).
// Copy is deep (the destination always owns a separate buffer).

#include <RTI/VariableLengthData.h>
#include <cstdlib>
#include <cstring>

namespace rti1516e {

// PIMPL holds the buffer + an ownership tag. The tag determines what the
// dtor must do: own → free, take → call deleter, borrow → leave alone.
class VariableLengthDataImplementation {
 public:
  enum Ownership { OWN, BORROW, TAKE, NONE };

  void* data{nullptr};
  size_t size{0};
  Ownership owns{NONE};
  VariableLengthDataDeleteFunction deleter{nullptr};

  VariableLengthDataImplementation() = default;

  ~VariableLengthDataImplementation() { release(); }

  void release() {
    if (owns == OWN && data != nullptr) {
      std::free(data);
    } else if (owns == TAKE && data != nullptr && deleter != nullptr) {
      deleter(data);
    }
    data = nullptr;
    size = 0;
    owns = NONE;
    deleter = nullptr;
  }

  // Copy-assign: deep-copy bytes; result is OWN.
  void copyFrom(VariableLengthDataImplementation const& rhs) {
    release();
    if (rhs.size > 0 && rhs.data != nullptr) {
      data = std::malloc(rhs.size);
      if (data == nullptr) {
        size = 0;
        owns = NONE;
        return;
      }
      std::memcpy(data, rhs.data, rhs.size);
      size = rhs.size;
      owns = OWN;
    } else {
      data = nullptr;
      size = 0;
      owns = NONE;
    }
  }
};

VariableLengthData::VariableLengthData()
    : _impl(new VariableLengthDataImplementation()) {}

VariableLengthData::VariableLengthData(void const* inData, size_t inSize)
    : _impl(new VariableLengthDataImplementation()) {
  setData(inData, inSize);
}

VariableLengthData::VariableLengthData(VariableLengthData const& rhs)
    : _impl(new VariableLengthDataImplementation()) {
  _impl->copyFrom(*rhs._impl);
}

VariableLengthData::~VariableLengthData() { delete _impl; }

VariableLengthData& VariableLengthData::operator=(
    VariableLengthData const& rhs) {
  if (this != &rhs) {
    _impl->copyFrom(*rhs._impl);
  }
  return *this;
}

void const* VariableLengthData::data() const { return _impl->data; }

size_t VariableLengthData::size() const { return _impl->size; }

void VariableLengthData::setData(void const* inData, size_t inSize) {
  _impl->release();
  if (inSize > 0 && inData != nullptr) {
    _impl->data = std::malloc(inSize);
    if (_impl->data == nullptr) {
      _impl->size = 0;
      _impl->owns = VariableLengthDataImplementation::NONE;
      return;
    }
    std::memcpy(_impl->data, inData, inSize);
    _impl->size = inSize;
    _impl->owns = VariableLengthDataImplementation::OWN;
  }
}

void VariableLengthData::setDataPointer(void* inData, size_t inSize) {
  _impl->release();
  _impl->data = inData;
  _impl->size = inSize;
  _impl->owns = VariableLengthDataImplementation::BORROW;
}

void VariableLengthData::takeDataPointer(
    void* inData, size_t inSize, VariableLengthDataDeleteFunction func) {
  _impl->release();
  _impl->data = inData;
  _impl->size = inSize;
  _impl->owns = VariableLengthDataImplementation::TAKE;
  _impl->deleter = func;
}

}  // namespace rti1516e
