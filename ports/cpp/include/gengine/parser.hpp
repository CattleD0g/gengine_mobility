// Public parser entry point for the C++ port.

#pragma once

#include "ast.hpp"

#include <stdexcept>
#include <string>
#include <vector>

namespace gengine {

// Thrown by parse() when the input is not a valid rule program.
class ParseError : public std::runtime_error {
public:
    using std::runtime_error::runtime_error;
};

// Tokenise + parse a full program. Throws ParseError on bad input.
std::vector<Rule> parse(const std::string& source);

} // namespace gengine
