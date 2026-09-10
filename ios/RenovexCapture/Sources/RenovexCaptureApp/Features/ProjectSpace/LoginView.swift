import SwiftUI
import UIKit

struct LoginView: View {
    let session: AppSession

    @State private var email = ""
    @State private var password = ""
    @State private var isSubmitting = false
    @State private var showPassword = false
    @State private var hasAppeared = false
    @State private var loginError: String?

    @FocusState private var focusedField: Field?

    private enum Field {
        case email
        case password
    }

    private let renovexOrange = Color(
        red: 1.0,
        green: 0.42,
        blue: 0.08
    )

    var body: some View {
        ZStack {
            background

            ScrollView {
                VStack(spacing: 0) {
                    Spacer(minLength: 80)

                    brandHeader
                        .padding(.bottom, 36)

                    loginCard

                    footer
                        .padding(.top, 24)

                    Spacer(minLength: 40)
                }
                .padding(.horizontal, 24)
                .frame(maxWidth: 520)
                .frame(maxWidth: .infinity)
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .ignoresSafeArea()
        .onAppear {
            withAnimation(
                .spring(
                    response: 0.75,
                    dampingFraction: 0.85
                )
                .delay(0.1)
            ) {
                hasAppeared = true
            }
        }
    }
}

// MARK: - Background

private extension LoginView {
    var background: some View {
        ZStack {
            Color.black

            LinearGradient(
                colors: [
                    Color.black,
                    Color(
                        red: 0.07,
                        green: 0.055,
                        blue: 0.045
                    ),
                    Color.black
                ],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )

            Circle()
                .fill(
                    renovexOrange
                        .opacity(0.22)
                        .blur(radius: 90)
                )
                .frame(width: 340, height: 340)
                .offset(x: 170, y: -290)

            Circle()
                .fill(
                    renovexOrange
                        .opacity(0.09)
                        .blur(radius: 100)
                )
                .frame(width: 300, height: 300)
                .offset(x: -180, y: 350)

            architecturalPattern
                .opacity(0.18)
        }
    }

    var architecturalPattern: some View {
        GeometryReader { geometry in
            Path { path in
                let spacing: CGFloat = 48

                stride(
                    from: 0,
                    through: geometry.size.width,
                    by: spacing
                ).forEach { x in
                    path.move(
                        to: CGPoint(x: x, y: 0)
                    )

                    path.addLine(
                        to: CGPoint(
                            x: x,
                            y: geometry.size.height
                        )
                    )
                }

                stride(
                    from: 0,
                    through: geometry.size.height,
                    by: spacing
                ).forEach { y in
                    path.move(
                        to: CGPoint(x: 0, y: y)
                    )

                    path.addLine(
                        to: CGPoint(
                            x: geometry.size.width,
                            y: y
                        )
                    )
                }
            }
            .stroke(
                Color.white.opacity(0.08),
                lineWidth: 0.5
            )
        }
        .mask(
            LinearGradient(
                colors: [
                    .clear,
                    .white,
                    .clear
                ],
                startPoint: .top,
                endPoint: .bottom
            )
        )
    }
}

// MARK: - Branding

private extension LoginView {
    var brandHeader: some View {
        VStack(spacing: 14) {
            ZStack {
                RoundedRectangle(cornerRadius: 22)
                    .fill(
                        LinearGradient(
                            colors: [
                                renovexOrange,
                                renovexOrange.opacity(0.72)
                            ],
                            startPoint: .topLeading,
                            endPoint: .bottomTrailing
                        )
                    )
                    .frame(width: 72, height: 72)

                Image(systemName: "building.2.fill")
                    .font(
                        .system(
                            size: 29,
                            weight: .semibold
                        )
                    )
                    .foregroundStyle(.black)
            }
            .shadow(
                color: renovexOrange.opacity(0.35),
                radius: 24,
                y: 8
            )

            VStack(spacing: 6) {
                Text("Renovex")
                    .font(
                        .system(
                            size: 34,
                            weight: .bold,
                            design: .rounded
                        )
                    )
                    .foregroundStyle(.white)

                Text("Build smarter. Manage better.")
                    .font(.subheadline)
                    .foregroundStyle(
                        .white.opacity(0.55)
                    )
            }
        }
        .opacity(hasAppeared ? 1 : 0)
        .offset(y: hasAppeared ? 0 : -18)
    }
}

// MARK: - Login Card

private extension LoginView {
    var loginCard: some View {
        VStack(spacing: 24) {
            VStack(
                alignment: .leading,
                spacing: 6
            ) {
                Text("Welcome back")
                    .font(.title2.bold())
                    .foregroundStyle(.white)

                Text(
                    "Sign in to continue managing your projects."
                )
                .font(.subheadline)
                .foregroundStyle(
                    .white.opacity(0.52)
                )
            }
            .frame(
                maxWidth: .infinity,
                alignment: .leading
            )

            VStack(spacing: 14) {
                emailField
                passwordField

                if let loginError {
                    loginErrorView(loginError)
                        .transition(
                            .opacity.combined(
                                with: .move(edge: .top)
                            )
                        )
                }
            }
            .animation(
                .easeInOut(duration: 0.2),
                value: loginError
            )

            HStack {
                Spacer()

                Button {
                    // Connect this to your
                    // password recovery flow later.
                } label: {
                    Text("Forgot password?")
                        .font(
                            .footnote.weight(
                                .semibold
                            )
                        )
                        .foregroundStyle(
                            renovexOrange
                        )
                }
                .buttonStyle(.plain)
            }

            signInButton
        }
        .padding(22)
        .background {
            RoundedRectangle(cornerRadius: 30)
                .fill(.ultraThinMaterial)
                .overlay {
                    RoundedRectangle(
                        cornerRadius: 30
                    )
                    .fill(
                        Color.black.opacity(0.28)
                    )
                }
                .overlay {
                    RoundedRectangle(
                        cornerRadius: 30
                    )
                    .stroke(
                        LinearGradient(
                            colors: [
                                renovexOrange
                                    .opacity(0.35),

                                Color.white
                                    .opacity(0.09),

                                Color.white
                                    .opacity(0.03)
                            ],
                            startPoint: .topLeading,
                            endPoint: .bottomTrailing
                        ),
                        lineWidth: 1
                    )
                }
        }
        .shadow(
            color: Color.black.opacity(0.4),
            radius: 35,
            y: 18
        )
        .opacity(hasAppeared ? 1 : 0)
        .offset(y: hasAppeared ? 0 : 32)
    }
}

// MARK: - Email Field

private extension LoginView {
    var emailField: some View {
        HStack(spacing: 14) {
            Image(systemName: "envelope.fill")
                .font(
                    .system(
                        size: 15,
                        weight: .semibold
                    )
                )
                .foregroundStyle(
                    focusedField == .email
                        ? renovexOrange
                        : Color.white.opacity(0.42)
                )
                .frame(width: 22)

            TextField(
                "",
                text: $email,
                prompt: Text("Email address")
                    .foregroundStyle(
                        .white.opacity(0.35)
                    )
            )
            .foregroundStyle(.white)
            .tint(renovexOrange)
            .textContentType(.username)
            .keyboardType(.emailAddress)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .focused(
                $focusedField,
                equals: .email
            )
            .submitLabel(.next)
            .onSubmit {
                focusedField = .password
            }
            .onChange(of: email) {
                clearLoginError()
            }
        }
        .padding(.horizontal, 18)
        .frame(height: 58)
        .background(
            fieldBackground(for: .email)
        )
        .animation(
            .easeOut(duration: 0.18),
            value: focusedField
        )
    }
}

// MARK: - Password Field

private extension LoginView {
    var passwordField: some View {
        HStack(spacing: 14) {
            Image(systemName: "lock.fill")
                .font(
                    .system(
                        size: 15,
                        weight: .semibold
                    )
                )
                .foregroundStyle(
                    focusedField == .password
                        ? renovexOrange
                        : Color.white.opacity(0.42)
                )
                .frame(width: 22)

            Group {
                if showPassword {
                    TextField(
                        "",
                        text: $password,
                        prompt: Text("Password")
                            .foregroundStyle(
                                .white.opacity(0.35)
                            )
                    )
                } else {
                    SecureField(
                        "",
                        text: $password,
                        prompt: Text("Password")
                            .foregroundStyle(
                                .white.opacity(0.35)
                            )
                    )
                }
            }
            .foregroundStyle(.white)
            .tint(renovexOrange)
            .textContentType(.password)
            .focused(
                $focusedField,
                equals: .password
            )
            .submitLabel(.go)
            .onSubmit {
                submit()
            }
            .onChange(of: password) {
                clearLoginError()
            }

            Button {
                showPassword.toggle()
            } label: {
                Image(
                    systemName:
                        showPassword
                        ? "eye.slash.fill"
                        : "eye.fill"
                )
                .font(
                    .system(
                        size: 15,
                        weight: .medium
                    )
                )
                .foregroundStyle(
                    .white.opacity(0.45)
                )
                .frame(
                    width: 30,
                    height: 30
                )
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal, 18)
        .frame(height: 58)
        .background(
            fieldBackground(for: .password)
        )
        .animation(
            .easeOut(duration: 0.18),
            value: focusedField
        )
    }

    func fieldBackground(
        for field: Field
    ) -> some View {
        RoundedRectangle(cornerRadius: 17)
            .fill(
                Color.white.opacity(0.055)
            )
            .overlay {
                RoundedRectangle(
                    cornerRadius: 17
                )
                .stroke(
                    loginError != nil
                        ? Color.red.opacity(0.65)
                        : focusedField == field
                            ? renovexOrange.opacity(0.8)
                            : Color.white.opacity(0.09),
                    lineWidth:
                        focusedField == field
                        || loginError != nil
                        ? 1.5
                        : 1
                )
            }
            .shadow(
                color:
                    focusedField == field
                    && loginError == nil
                    ? renovexOrange.opacity(0.12)
                    : .clear,
                radius: 12
            )
    }
}

// MARK: - Login Error

private extension LoginView {
    func loginErrorView(
        _ message: String
    ) -> some View {
        HStack(spacing: 9) {
            Image(
                systemName:
                    "exclamationmark.circle.fill"
            )
            .font(.system(size: 14))

            Text(message)
                .font(
                    .footnote.weight(.medium)
                )

            Spacer()
        }
        .foregroundStyle(
            Color.red.opacity(0.9)
        )
        .padding(.horizontal, 4)
    }

    func clearLoginError() {
        guard loginError != nil else {
            return
        }

        withAnimation(
            .easeOut(duration: 0.15)
        ) {
            loginError = nil
        }
    }
}

// MARK: - Sign In

private extension LoginView {
    var signInButton: some View {
        Button {
            submit()
        } label: {
            HStack(spacing: 10) {
                if isSubmitting {
                    ProgressView()
                        .tint(.black)

                    Text("Signing in")
                } else {
                    Text("Sign in")

                    Image(
                        systemName: "arrow.right"
                    )
                    .font(
                        .system(
                            size: 14,
                            weight: .bold
                        )
                    )
                }
            }
            .font(.headline)
            .foregroundStyle(.black)
            .frame(maxWidth: .infinity)
            .frame(height: 58)
            .background {
                RoundedRectangle(
                    cornerRadius: 18
                )
                .fill(
                    LinearGradient(
                        colors: [
                            renovexOrange,
                            Color(
                                red: 1.0,
                                green: 0.55,
                                blue: 0.12
                            )
                        ],
                        startPoint: .leading,
                        endPoint: .trailing
                    )
                )
            }
            .shadow(
                color: renovexOrange.opacity(
                    canSubmit ? 0.28 : 0
                ),
                radius: 18,
                y: 8
            )
        }
        .buttonStyle(
            RenovexButtonStyle()
        )
        .disabled(!canSubmit)
        .opacity(canSubmit ? 1 : 0.45)
        .animation(
            .easeInOut(duration: 0.2),
            value: canSubmit
        )
    }

    var canSubmit: Bool {
        !email
            .trimmingCharacters(
                in: .whitespacesAndNewlines
            )
            .isEmpty
        && !password.isEmpty
        && !isSubmitting
    }

    func submit() {
        guard canSubmit else {
            return
        }

        focusedField = nil

        withAnimation {
            loginError = nil
        }

        Task {
            isSubmitting = true

            let result = await session.login(
                email: email.trimmingCharacters(
                        in: .whitespacesAndNewlines
                    ),
                password: password
            )

            isSubmitting = false

            switch result {
            case .success:
                UINotificationFeedbackGenerator()
                    .notificationOccurred(.success)
                
                case .invalidCredentials: 
                    showLoginError(
                        "Incorrect email or password."
                    )

                    focusedField = .password 
                
                case .networkUnavailable: 
                    showLoginError(
                        "Network unavailable. Please check your connection."
                    )
                
                case .timedOut: 
                    showLoginError(
                        "Request timed out. Please try again."
                    )
                
                case .serverError:
                    showLoginError(
                        "Server error. Please try again later."
                    )
                
                case .unknownError:
                    showLoginError(
                        "An unknown error occurred. Please try again."
                    )
            }
        }
    }
    func showLoginError(_ message: String) {
    withAnimation(
        .spring(
            response: 0.35,
            dampingFraction: 0.75
        )
    ) {
        loginError = message
    }

    UINotificationFeedbackGenerator()
        .notificationOccurred(.error)
    }
}

// MARK: - Footer

private extension LoginView {
    var footer: some View {
        HStack(spacing: 7) {
            Image(
                systemName: "lock.shield.fill"
            )
            .font(.caption)

            Text("Secure access to Renovex")
                .font(.caption)
        }
        .foregroundStyle(
            .white.opacity(0.32)
        )
        .opacity(
            hasAppeared ? 1 : 0
        )
    }
}

// MARK: - Button Interaction

private struct RenovexButtonStyle:
    ButtonStyle {

    func makeBody(
        configuration: Configuration
    ) -> some View {
        configuration.label
            .scaleEffect(
                configuration.isPressed
                    ? 0.975
                    : 1
            )
            .brightness(
                configuration.isPressed
                    ? -0.05
                    : 0
            )
            .animation(
                .spring(
                    response: 0.25,
                    dampingFraction: 0.75
                ),
                value:
                    configuration.isPressed
            )
    }
}
